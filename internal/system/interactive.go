package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// InteractiveRunner delegates secret prompts to the macOS utility.
// No passphrase is accepted as an argument, captured, or logged.
type InteractiveRunner interface {
	RunInteractive(context.Context, string, ...string) error
}

func (ExecRunner) RunInteractive(ctx context.Context, executable string, args ...string) error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("interactive APFS operation requires a terminal: %w", err)
	}
	defer tty.Close()

	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = tty
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", executable, err)
	}
	return nil
}
