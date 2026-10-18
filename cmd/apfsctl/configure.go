package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmeiracorbal/apfsctl/internal/apfs"
	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/policy"
	"github.com/jmeiracorbal/apfsctl/internal/reconcile"
	"github.com/jmeiracorbal/apfsctl/internal/state"
	"github.com/jmeiracorbal/apfsctl/internal/system"
)

func runConfigure(ctx context.Context, args []string, client apfs.Client, output, stderr io.Writer) error {
	fs := flag.NewFlagSet("configure", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "repos", "volume name")
	container := fs.String("container", "", "APFS container UUID")
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := fs.String("mount", filepath.Join(home, "repos"), "existing mounted APFS path")
	owner := fs.Int("uid", -1, "nonnegative UID for volume root, -1 to preserve ownership")
	spotlight := fs.String("spotlight", "off", "off or keep")
	events := fs.String("fsevents", "keep", "keep or disable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *owner < -1 || (*spotlight != "off" && *spotlight != "keep") || (*events != "keep" && *events != "disable") {
		return errors.New("invalid uid, spotlight, or fsevents policy")
	}
	if os.Geteuid() != 0 {
		return errors.New("configure requires root privileges")
	}
	d := domain.Desired{Name: *name, ContainerUUID: *container, MountPoint: *path, Encrypted: true}
	if err := d.Validate(); err != nil {
		return err
	}
	snap, err := client.Snapshot(ctx)
	if err != nil {
		return err
	}
	p, err := reconcile.Build(d, snap)
	if err != nil {
		return err
	}
	if !p.Noop {
		return errors.New("configure requires the encrypted APFS volume mounted at the desired path")
	}
	var uuid string
	for _, vol := range snap.Volumes {
		if vol.Name == d.Name && strings.EqualFold(vol.ContainerUUID, d.ContainerUUID) && vol.MountPoint == d.MountPoint {
			uuid = vol.UUID
			break
		}
	}
	if uuid == "" {
		return fmt.Errorf("unable to identify mounted APFS volume %q", d.Name)
	}
	verify := func(ctx context.Context) error {
		current, err := client.Snapshot(ctx)
		if err != nil {
			return err
		}
		plan, err := reconcile.Build(d, current)
		if err != nil {
			return err
		}
		if !plan.Noop {
			return errors.New("APFS mount changed during policy reconciliation")
		}
		for _, v := range current.Volumes {
			if v.Name == d.Name && strings.EqualFold(v.ContainerUUID, d.ContainerUUID) &&
				v.MountPoint == d.MountPoint && strings.EqualFold(v.UUID, uuid) {
				return nil
			}
		}
		return errors.New("APFS volume identity changed during policy reconciliation")
	}
	service := policy.Service{
		Platform:    policy.MacOS{Run: system.ExecRunner{}},
		Lock:        state.FileLock{Path: "/private/var/db/apfsctl/operation.lock"},
		VerifyMount: verify,
	}
	result, err := service.Apply(ctx, policy.Desired{
		VolumeUUID:      uuid,
		MountPoint:      d.MountPoint,
		OwnerUID:        *owner,
		SpotlightOff:    *spotlight == "off",
		DisableFSEvents: *events == "disable",
	})
	if err != nil {
		return err
	}
	return printJSON(output, result)
}
