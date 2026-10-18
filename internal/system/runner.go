package system

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Runner executes an absolute executable with a typed argument vector, never a shell.
type Runner interface {
	Run(context.Context, string, []byte, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, executable string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", executable, err, bytes.TrimSpace(out))
	}
	return out, nil
}
