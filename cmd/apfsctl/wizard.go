package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/jmeiracorbal/apfsctl/internal/apfs"
	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/provision"
	"github.com/jmeiracorbal/apfsctl/internal/reconcile"
	"github.com/jmeiracorbal/apfsctl/internal/state"
	"github.com/jmeiracorbal/apfsctl/internal/system"
)

func runWizard(ctx context.Context, client apfs.Client) error {
	if os.Geteuid() != 0 {
		return errors.New("APFS setup requires administrator access; run `sudo apfsctl`")
	}

	dialog := system.AppleScriptPassphraseDialog{}
	rawName, err := dialog.PromptText(ctx, "APFS volume name (special characters become hyphens):")
	if err != nil {
		return err
	}
	name, err := sanitizeVolumeName(rawName)
	if err != nil {
		return err
	}
	mountPoint := filepath.Join("/Volumes", name)

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
	}
	plan, err := reconcile.Build(desired, snapshot)
	if err != nil {
		return err
	}
	if plan.Noop {
		return dialog.ShowMessage(ctx, "apfsctl", fmt.Sprintf("Volume %q is already ready at %s.", name, mountPoint))
	}
	if err := ensureSafeMountTarget(mountPoint, plan); err != nil {
		return err
	}
	if err := protectMountTarget(mountPoint); err != nil {
		return err
	}

	source := system.PassphraseSource(system.AppleScriptPassphraseDialog{})
	if hasPlannedAction(plan, reconcile.CreateEncrypted) {
		source = system.ConfirmingPassphraseSource{Dialog: dialog}
	}
	credentials := system.NewCredentialSession(source)
	defer credentials.Close()
	service := provision.Service{
		Platform:      apfs.Mutator{Read: client, Credentials: credentials},
		Journal:       state.FileJournal{Directory: "/private/var/db/apfsctl"},
		Lock:          state.FileLock{Path: "/private/var/db/apfsctl/operation.lock"},
		ValidateMount: validateWritableMountTarget,
	}
	if _, err := service.Apply(ctx, desired, true); err != nil {
		return err
	}
	return dialog.ShowMessage(ctx, "apfsctl", fmt.Sprintf("Volume %q is ready at %s.", name, mountPoint))
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
