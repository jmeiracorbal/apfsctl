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
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", executable, err)
	}
	return nil
}
