package apfs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmeiracorbal/apfsctl/internal/domain"
	"github.com/jmeiracorbal/apfsctl/internal/mount"
	"github.com/jmeiracorbal/apfsctl/internal/system"
)

// Interactive must be explicitly supplied to enable mutations.
type Mutator struct {
	Read        Client
	Credentials system.CredentialRunner
}

func (m Mutator) Snapshot(ctx context.Context) (domain.Snapshot, error) {
	return m.Read.Snapshot(ctx)
}

func (m Mutator) CreateEncrypted(ctx context.Context, container domain.Container, name string) error {
	if m.Credentials == nil {
		return errors.New("APFS credential runner not configured")
	}
	if err := (domain.Desired{Name: name, ContainerUUID: container.UUID, MountPoint: "/unused", Encrypted: true}).Validate(); err != nil {
		return err
	}
	// APFS is encrypted atomically from creation; -nomount avoids surprising mounts.
	// diskutil reads the secret from stdin, allowing both hidden terminal input
	// and non-interactive secret pipes without exposing it in process arguments.
	if err := m.Credentials.RunWithPassphrase(ctx, "New APFS volume passphrase: ", diskutil,
		"apfs", "addVolume", container.Reference, "APFS", name, "-stdinpassphrase", "-nomount"); err != nil {
		return fmt.Errorf("create encrypted APFS volume: %w", err)
	}
	return nil
}

func (m Mutator) Unlock(ctx context.Context, volume domain.Volume) error {
	if m.Credentials == nil {
		return errors.New("APFS credential runner not configured")
	}
	if err := m.Credentials.RunWithPassphrase(ctx, "APFS volume passphrase: ", diskutil,
		"apfs", "unlockVolume", volume.Device, "-stdinpassphrase", "-nomount"); err != nil {
		return fmt.Errorf("unlock APFS volume: %w", err)
	}
	return nil
}

func (m Mutator) Mount(ctx context.Context, volume domain.Volume, mountPoint string, browsable bool) error {
	if m.Read.Run == nil {
		return errors.New("APFS runner not configured")
	}
	args := []string{"mount"}
	if !browsable {
		args = append(args, "nobrowse")
	}
	args = append(args, "-mountOptions", mount.DiskutilOptions,
		"-mountPoint", mountPoint, volume.Device)
	_, err := m.Read.Run.Run(ctx, diskutil, nil, args...)
	if err != nil {
		return fmt.Errorf("mount APFS volume: %w", err)
	}
	return nil
}

func (m Mutator) Unmount(ctx context.Context, volume domain.Volume) error {
	if m.Read.Run == nil {
		return errors.New("APFS runner not configured")
	}
	if _, err := m.Read.Run.Run(ctx, diskutil, nil, "unmount", volume.Device); err != nil {
		return fmt.Errorf("unmount APFS volume: %w", err)
	}
	return nil
}
