package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/jmeiracorbal/apfsctl/internal/apfs"
	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/policy"
	"github.com/jmeiracorbal/apfsctl/internal/provision"
	"github.com/jmeiracorbal/apfsctl/internal/reconcile"
	"github.com/jmeiracorbal/apfsctl/internal/state"
	"github.com/jmeiracorbal/apfsctl/internal/system"
)

func runWizard(ctx context.Context, client apfs.Client, progress io.Writer) error {
	if os.Geteuid() != 0 {
		return errors.New("APFS setup requires administrator access; launch apfsctl from a terminal")
	}

	logProgress(progress, "Starting setup.")
	dialog := system.AppleScriptPassphraseDialog{}
	logProgress(progress, "Waiting for the volume name dialog...")
	rawName, err := dialog.PromptText(ctx, "APFS volume name (special characters become hyphens):")
	if err != nil {
		return err
	}
	name, err := sanitizeVolumeName(rawName)
	if err != nil {
		return err
	}
	mountPoint := filepath.Join("/Volumes", name)
	logProgress(progress, "Waiting for the Finder visibility choice...")
	visibility, err := dialog.PromptChoice(ctx, "apfsctl", "Show this volume in Finder?", "Hidden", "Visible")
	if err != nil {
		return err
	}
	logProgress(progress, "Waiting for the Spotlight preference...")
	spotlight, err := dialog.PromptChoice(ctx, "apfsctl", "Spotlight indexing", "Keep current", "Enable", "Disable")
	if err != nil {
		return err
	}
	logProgress(progress, "Waiting for the FSEvents preference...")
	events, err := dialog.PromptChoice(ctx, "apfsctl", "Persistent FSEvents logging", "Keep current", "Enable", "Disable")
	if err != nil {
		return err
	}
	spotlightSetting := map[string]string{"Keep current": "keep", "Enable": "on", "Disable": "off"}[spotlight]
	eventsSetting := map[string]string{"Keep current": "keep", "Enable": "enable", "Disable": "disable"}[events]

	logProgress(progress, "Reading current APFS state...")
	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		return err
	}
	container, err := activeSystemContainer(snapshot)
	if err != nil {
		return err
	}
	desired := domain.Desired{
		Name: name, ContainerUUID: container.UUID, MountPoint: mountPoint, Encrypted: true,
		Browsable: visibility == "Visible",
	}
	plan, err := reconcile.Build(desired, snapshot)
	if err != nil {
		return err
	}
	if plan.Noop {
		logProgress(progress, "The volume is already mounted with the requested volume settings.")
	} else {
		logProgress(progress, "Reconciling the APFS volume configuration...")
	}
	if hasPlannedAction(plan, reconcile.Mount) && !hasPlannedAction(plan, reconcile.Unmount) {
		logProgress(progress, "Preparing and protecting the mount point...")
		if err := ensureSafeMountTarget(mountPoint, plan); err != nil {
			return err
		}
		if err := protectMountTarget(mountPoint); err != nil {
			return err
		}
	}

	source := system.PassphraseSource(system.AppleScriptPassphraseDialog{})
	if hasPlannedAction(plan, reconcile.CreateEncrypted) {
		source = system.ConfirmingPassphraseSource{Dialog: dialog}
	} else if hasPlannedAction(plan, reconcile.Unlock) {
		source = system.PassphraseSource(dialog)
	}
	credentials := system.NewCredentialSession(source)
	defer credentials.Close()
	service := provision.Service{
		Platform:      apfs.Mutator{Read: client, Credentials: credentials},
		Journal:       state.FileJournal{Directory: "/private/var/db/apfsctl"},
		Lock:          state.FileLock{Path: "/private/var/db/apfsctl/operation.lock"},
		ValidateMount: prepareWritableMountTarget,
	}
	logProgress(progress, "Applying APFS volume changes. A passphrase prompt may appear if needed...")
	result, err := service.Apply(ctx, desired, true)
	if err != nil {
		return err
	}
	if result.VolumeUUID == "" {
		return errors.New("reconciliation completed without identifying the APFS volume")
	}
	policies := policy.Service{
		Platform: policy.MacOS{Run: system.ExecRunner{}},
		Lock:     state.FileLock{Path: "/private/var/db/apfsctl/operation.lock"},
		VerifyMount: func(ctx context.Context) error {
			current, err := client.Snapshot(ctx)
			if err != nil {
				return err
			}
			for _, volume := range current.Volumes {
				if volume.Name == name && volume.UUID == result.VolumeUUID && volume.MountPoint == mountPoint &&
					volume.Browsable != nil && *volume.Browsable == desired.Browsable {
					return nil
				}
			}
			return errors.New("APFS volume identity or mount changed before applying policies")
		},
	}
	logProgress(progress, "Checking and reconciling Finder, Spotlight, and FSEvents settings...")
	_, err = policies.Apply(ctx, policy.Desired{
		VolumeUUID: result.VolumeUUID, MountPoint: mountPoint,
		Browsable: desired.Browsable, OwnerUID: -1,
		Spotlight: spotlightSetting, FSEvents: eventsSetting,
	})
	if err != nil {
		return err
	}
	completion := wizardCompletionMessage(
		name, mountPoint, hasPlannedAction(plan, reconcile.CreateEncrypted), desired.Browsable,
		spotlightSetting, eventsSetting,
	)
	logProgress(progress, "Setup complete. Volume ready at %s.", mountPoint)
	return dialog.ShowMessage(ctx, "apfsctl", completion)
}

func logProgress(output io.Writer, format string, args ...any) {
	if output == nil {
		output = os.Stderr
	}
	_, _ = fmt.Fprintf(output, "apfsctl: "+format+"\n", args...)
}

func wizardCompletionMessage(name, mountPoint string, created, browsable bool, spotlight, events string) string {
	status := "Volume already existed; configuration reconciled"
	if created {
		status = "Created encrypted APFS volume"
	}
	visibility := "hidden"
	if browsable {
		visibility = "visible"
	}
	return fmt.Sprintf("%s %q.\n\nFinder: %s\nencryption: enabled\nSpotlight: %s\nFSEvents: %s\n\nReady at %s.",
		status, name, visibility, policySettingLabel(spotlight), policySettingLabel(events), mountPoint)
}

func policySettingLabel(setting string) string {
	switch setting {
	case "keep":
		return "unchanged"
	case "on", "enable":
		return "enabled"
	case "off", "disable":
		return "disabled"
	default:
		return "unknown"
	}
}

func activeSystemContainer(snapshot domain.Snapshot) (domain.Container, error) {
	var matches []domain.Volume
	for _, volume := range snapshot.Volumes {
		if volume.MountPoint == "/System/Volumes/Data" && containsRole(volume.Roles, "Data") && !volume.Locked {
			matches = append(matches, volume)
		}
	}
	if len(matches) != 1 {
		return domain.Container{}, errors.New("cannot identify one active macOS Data volume; refusing to guess the APFS container")
	}
	for _, container := range snapshot.Containers {
		if strings.EqualFold(container.UUID, matches[0].ContainerUUID) {
			return container, nil
		}
	}
	return domain.Container{}, errors.New("active macOS Data volume refers to an unknown APFS container")
}

func containsRole(roles []string, role string) bool {
	for _, candidate := range roles {
		if strings.EqualFold(candidate, role) {
			return true
		}
	}
	return false
}

func sanitizeVolumeName(input string) (string, error) {
	input = strings.TrimSpace(input)
	var result strings.Builder
	separator := false
	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || strings.ContainsRune("._-", r) {
			if separator && result.Len() > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(r)
			separator = false
			continue
		}
		separator = true
	}
	name := strings.Trim(result.String(), ".-_")
	if name == "" {
		return "", errors.New("volume name must include at least one letter or number")
	}
	if err := (domain.Desired{
		Name: name, ContainerUUID: "pending", MountPoint: filepath.Join("/Volumes", name), Encrypted: true,
	}).Validate(); err != nil {
		return "", err
	}
	return name, nil
}

func hasPlannedAction(plan reconcile.Plan, action reconcile.Action) bool {
	for _, change := range plan.Changes {
		if change.Action == action {
			return true
		}
	}
	return false
}
