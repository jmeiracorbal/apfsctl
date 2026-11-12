package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jmeiracorbal/apfsctl/internal/apfs"
	"github.com/jmeiracorbal/apfsctl/internal/diagnostic"
	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/mount"
	"github.com/jmeiracorbal/apfsctl/internal/policy"
	"github.com/jmeiracorbal/apfsctl/internal/provision"
	"github.com/jmeiracorbal/apfsctl/internal/reconcile"
	"github.com/jmeiracorbal/apfsctl/internal/state"
	"github.com/jmeiracorbal/apfsctl/internal/system"
)

func main() {
	args := os.Args[1:]
	wizard := len(args) == 0 || args[0] == "setup"
	if err := run(args, os.Stdout, os.Stderr); err != nil {
		if wizard {
			if errors.Is(err, system.ErrDialogCanceled) {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			showErr := (system.AppleScriptPassphraseDialog{}).ShowMessage(ctx, "apfsctl", err.Error())
			cancel()
			if showErr == nil {
				os.Exit(1)
			}
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 && args[0] == "version" {
		_, err := fmt.Fprintln(stdout, version)
		return err
	}
	if len(args) == 0 {
		args = []string{"setup"}
	}
	if os.Getenv("APFSCTL_VIFS_UUID") != "" {
		if len(args) != 1 {
			return errors.New("fstab editor requires exactly one temporary file")
		}
		browsable, err := strconv.ParseBool(os.Getenv("APFSCTL_VIFS_BROWSABLE"))
		if err != nil {
			return errors.New("fstab editor requires an explicit visibility setting")
		}
		return mount.EditTemporary(args[0], mount.Entry{
			UUID: os.Getenv("APFSCTL_VIFS_UUID"), Path: os.Getenv("APFSCTL_VIFS_PATH"), Browsable: browsable,
		})
	}
	if runtime.GOOS != "darwin" {
		return errors.New("apfsctl requires macOS (APFS); unit tests support other platforms")
	}
	timeout := 45 * time.Second
	if args[0] == "create" || args[0] == "mount" || args[0] == "configure" || args[0] == "setup" {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client := apfs.Client{Run: system.ExecRunner{}}
	switch args[0] {
	case "setup":
		if len(args) != 1 {
			return errors.New("setup takes no arguments; run apfsctl setup")
		}
		if os.Geteuid() != 0 {
			return system.ReexecWithSudo(ctx, args, stdout, stderr)
		}
		return runWizard(ctx, client, stderr)
	case "configure":
		return runConfigure(ctx, args[1:], client, stdout, stderr)
	case "status":
		if len(args) != 1 {
			return errors.New("unexpected arguments")
		}
		s, err := client.Snapshot(ctx)
		if err != nil {
			return err
		}
		return printJSON(stdout, s)
	case "plan", "doctor", "create", "mount":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(stderr)
		name := flags.String("name", "repos", "APFS volume name")
		container := flags.String("container", "", "APFS container UUID")
		visibility := flags.String("visibility", "", "required Finder visibility: hidden or visible")
		spotlight := flags.String("spotlight", "keep", "create policy: keep, on, or off")
		events := flags.String("fsevents", "keep", "create policy: keep, enable, or disable")
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		mount := flags.String("mount", filepath.Join(home, "repos"), "absolute mount directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected positional arguments")
		}
		var desired *domain.Desired
		needsDesired := args[0] == "plan" || args[0] == "create" || args[0] == "mount" || *container != ""
		if needsDesired && *visibility != "hidden" && *visibility != "visible" {
			return errors.New("--visibility is required and must be hidden or visible")
		}
		if (*spotlight != "keep" && *spotlight != "on" && *spotlight != "off") ||
			(*events != "keep" && *events != "enable" && *events != "disable") {
			return errors.New("--spotlight must be keep, on, or off; --fsevents must be keep, enable, or disable")
		}
		if needsDesired {
			d := domain.Desired{Name: *name, ContainerUUID: *container, MountPoint: *mount, Encrypted: true, Browsable: *visibility == "visible"}
			if err := d.Validate(); err != nil {
				return err
			}
			desired = &d
		}
		s, err := client.Snapshot(ctx)
		if err != nil {
			return err
		}
		if args[0] == "doctor" {
			report := diagnostic.Inspect(s, desired)
			if report.Plan != nil && !report.Plan.Noop {
				if err := ensureSafeMountTarget(*mount, *report.Plan); err != nil {
					report.Healthy = false
					report.Findings = append(report.Findings, diagnostic.Finding{Code: "mount_target", Message: err.Error()})
				}
			}
			if err := printJSON(stdout, report); err != nil {
				return err
			}
			if !report.Healthy {
				return errors.New("diagnostics detected configuration drift or conflict")
			}
			return nil
		}
		p, err := reconcile.Build(*desired, s)
		if err != nil {
			return err
		}
		if !p.Noop && hasPlannedAction(p, reconcile.Mount) && !hasPlannedAction(p, reconcile.Unmount) {
			if err := ensureSafeMountTarget(p.Desired.MountPoint, p); err != nil {
				return err
			}
		}
		if args[0] == "create" || args[0] == "mount" {
			if (args[0] == "create" || !p.Noop) && os.Geteuid() != 0 {
				return system.ReexecWithSudo(ctx, args, stdout, stderr)
			}
			if !p.Noop && hasPlannedAction(p, reconcile.Mount) && !hasPlannedAction(p, reconcile.Unmount) {
				if err := protectMountTarget(*mount); err != nil {
					return err
				}
			}
			credentials := system.NewCredentialSession(system.TerminalPassphraseSource{})
			defer credentials.Close()
			svc := provision.Service{
				Platform:      apfs.Mutator{Read: client, Credentials: credentials},
				Journal:       state.FileJournal{Directory: "/private/var/db/apfsctl"},
				Lock:          state.FileLock{Path: "/private/var/db/apfsctl/operation.lock"},
				ValidateMount: prepareWritableMountTarget,
			}
			result, err := svc.Apply(ctx, *desired, args[0] == "create")
			if err != nil {
				return err
			}
			if args[0] == "create" {
				policies := policy.Service{
					Platform: policy.MacOS{Run: system.ExecRunner{}},
					Lock:     state.FileLock{Path: "/private/var/db/apfsctl/operation.lock"},
					VerifyMount: func(ctx context.Context) error {
						current, err := client.Snapshot(ctx)
						if err != nil {
							return err
						}
						for _, volume := range current.Volumes {
							if volume.Name == desired.Name && strings.EqualFold(volume.UUID, result.VolumeUUID) &&
								volume.MountPoint == desired.MountPoint && volume.Browsable != nil && *volume.Browsable == desired.Browsable {
								return nil
							}
						}
						return errors.New("APFS volume identity or mount changed before applying policies")
					},
				}
				policyResult, err := policies.Apply(ctx, policy.Desired{
					VolumeUUID: result.VolumeUUID, MountPoint: desired.MountPoint,
					Browsable: desired.Browsable, OwnerUID: -1,
					Spotlight: *spotlight, FSEvents: *events,
				})
				if err != nil {
					return err
				}
				return printJSON(stdout, struct {
					Volume   provision.Result `json:"volume"`
					Policies policy.Result    `json:"policies"`
				}{Volume: result, Policies: policyResult})
			}
			return printJSON(stdout, result)
		}
		return printJSON(stdout, p)
	case "unmount", "remove":
		return fmt.Errorf("%s is not implemented: privileged mutations are disabled in this skeleton", args[0])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// ensureSafeMountTarget checks directory ownership of the path, not APFS state.
// This is a read-only safety check, invoked only if the planned operation mounts.
func ensureSafeMountTarget(path string, p reconcile.Plan) error {
	needsMount := false
	for _, change := range p.Changes {
		if change.Action == reconcile.Mount {
			needsMount = true
		}
	}
	if !needsMount {
		return nil
	}
	for current := path; current != "/"; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("mount path contains symlink %q", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("mount path component %q is not a directory", current)
		}
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	dirStat, dirOK := info.Sys().(*syscall.Stat_t)
	parentStat, parentOK := parent.Sys().(*syscall.Stat_t)
	if !dirOK || !parentOK {
		return errors.New("cannot verify mount point filesystem")
	}
	if dirStat.Dev != parentStat.Dev {
		return fmt.Errorf("mount point %q is already a filesystem mount", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("mount point %q is not empty; refusing to hide existing files", path)
	}
	return nil
}

// protectMountTarget keeps writes from silently landing on the startup volume
// while the APFS volume is absent.
func protectMountTarget(path string) error {
	plan := reconcile.Plan{Changes: []reconcile.Change{{Action: reconcile.Mount}}}
	if err := ensureSafeMountTarget(path, plan); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0700); err != nil {
			return fmt.Errorf("create mount directory %q: %w", path, err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("mount point is not a directory")
	}
	if err := os.Chown(path, 0, 0); err != nil {
		return fmt.Errorf("protect unmounted mount directory ownership: %w", err)
	}
	if err := os.Chmod(path, 0); err != nil {
		return fmt.Errorf("protect unmounted mount directory permissions: %w", err)
	}
	return nil
}

func prepareWritableMountTarget(path string) error {
	if err := protectMountTarget(path); err != nil {
		return err
	}
	return validateWritableMountTarget(path)
}

// validateWritableMountTarget checks the already-prepared, empty mount target.
func validateWritableMountTarget(path string) error {
	plan := reconcile.Plan{Changes: []reconcile.Change{{Action: reconcile.Mount}}}
	if err := ensureSafeMountTarget(path, plan); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("mount point is not a directory")
	}
	return nil
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
