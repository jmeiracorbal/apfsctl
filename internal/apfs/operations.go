package apfs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/system"
)

// Interactive must be explicitly supplied to enable mutations.
type Mutator struct {
	Read        Client
	Interactive system.InteractiveRunner
}

func (m Mutator) Snapshot(ctx context.Context) (domain.Snapshot, error) {
	return m.Read.Snapshot(ctx)
}

func (m Mutator) CreateEncrypted(ctx context.Context, container domain.Container, name string) error {
	if m.Interactive == nil {
		return errors.New("interactive APFS runner not configured")
	}
	if err := (domain.Desired{Name: name, ContainerUUID: container.UUID, MountPoint: "/unused", Encrypted: true}).Validate(); err != nil {
		return err
	}
	// APFS is encrypted atomically from creation; -nomount avoids surprising mounts.
	// The passphrase is prompted by diskutil, never held by apfsctl.
	if err := m.Interactive.RunInteractive(ctx, diskutil, "apfs", "addVolume", container.Reference,
		"APFS", name, "-passprompt", "-nomount"); err != nil {
		return fmt.Errorf("create encrypted APFS volume: %w", err)
	}
	return nil
}

func (m Mutator) Unlock(ctx context.Context, volume domain.Volume, mountPoint string) error {
	if m.Interactive == nil {
		return errors.New("interactive APFS runner not configured")
	}
	// Omitting -passphrase causes diskutil to prompt safely on the terminal.
	if err := m.Interactive.RunInteractive(ctx, diskutil, "apfs", "unlockVolume",
		volume.Device, "-mountpoint", mountPoint); err != nil {
		return fmt.Errorf("unlock APFS volume: %w", err)
	}
	return nil
}

func (m Mutator) Mount(ctx context.Context, volume domain.Volume, mountPoint string) error {
	if m.Read.Run == nil {
		return errors.New("APFS runner not configured")
	}
	_, err := m.Read.Run.Run(ctx, diskutil, nil, "mount", "-mountPoint", mountPoint, volume.Device)
	if err != nil {
		return fmt.Errorf("mount APFS volume: %w", err)
	}
	return nil
}
