package system

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// ReexecWithSudo relaunches the current CLI with administrator privileges.
// It passes arguments and standard streams directly to sudo without a shell.
func ReexecWithSudo(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate apfsctl executable: %w", err)
	}
	cmd := sudoCommand(ctx, executable, args)
	cmd.Stdin = os.Stdin
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("request administrator privileges: %w", err)
	}
	return nil
}

func sudoCommand(ctx context.Context, executable string, args []string) *exec.Cmd {
	commandArgs := make([]string, 1, len(args)+1)
	commandArgs[0] = executable
	commandArgs = append(commandArgs, args...)
	return exec.CommandContext(ctx, "/usr/bin/sudo", commandArgs...)
}
