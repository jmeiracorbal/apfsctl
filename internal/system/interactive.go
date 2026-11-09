package system

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// CredentialRunner sends the operation's passphrase to a system utility over stdin.
// Passphrases are never included in arguments, environment variables, or logs.
type CredentialRunner interface {
	RunWithPassphrase(context.Context, string, string, ...string) error
}

type PassphraseSource interface {
	ReadPassphrase(context.Context, string) ([]byte, error)
}

type PassphraseDialog interface {
	ReadPassphrase(context.Context, string) ([]byte, error)
}

type TerminalPassphraseSource struct {
	Input  *os.File
	Output io.Writer
	Dialog PassphraseDialog
}

func (s TerminalPassphraseSource) ReadPassphrase(ctx context.Context, prompt string) ([]byte, error) {
	input := s.Input
	if input == nil {
		input = os.Stdin
	}
	output := s.Output
	if output == nil {
		output = os.Stderr
	}
	secret, err := readPassphrase(input, output, prompt)
	if !errors.Is(err, errNoPassphraseOnStdin) {
		return secret, err
	}
	dialog := s.Dialog
	if dialog == nil {
		dialog = AppleScriptPassphraseDialog{}
	}
	return dialog.ReadPassphrase(ctx, prompt)
}

// CredentialSession reads one passphrase and reuses it across the steps of a
// single create/mount operation. Close it as soon as the operation completes.
type CredentialSession struct {
	source PassphraseSource
	secret []byte
}

func NewCredentialSession(source PassphraseSource) *CredentialSession {
	return &CredentialSession{source: source}
}

func (s *CredentialSession) RunWithPassphrase(ctx context.Context, prompt, executable string, args ...string) error {
	if s == nil || s.source == nil {
		return errors.New("passphrase source not configured")
	}
	if s.secret == nil {
		secret, err := s.source.ReadPassphrase(ctx, prompt)
		if err != nil {
			return err
		}
		s.secret = secret
	}

	input := make([]byte, 0, len(s.secret)+1)
	input = append(input, s.secret...)
	input = append(input, '\n')
	defer clear(input)

	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", executable, err)
	}
	return nil
}

func (s *CredentialSession) Close() {
	if s == nil {
		return
	}
	clear(s.secret)
	s.secret = nil
}

var errNoPassphraseOnStdin = errors.New("no passphrase provided on stdin")
var ErrDialogCanceled = errors.New("dialog canceled")

type AppleScriptPassphraseDialog struct{}

func (AppleScriptPassphraseDialog) ReadPassphrase(ctx context.Context, prompt string) ([]byte, error) {
	return runAppleScriptPrompt(ctx, prompt, true)
}

func (AppleScriptPassphraseDialog) PromptText(ctx context.Context, prompt string) (string, error) {
	answer, err := runAppleScriptPrompt(ctx, prompt, false)
	if err != nil {
		return "", err
	}
	defer clear(answer)
	return string(answer), nil
}

func (AppleScriptPassphraseDialog) PromptChoice(ctx context.Context, title, message string, choices ...string) (string, error) {
	if len(choices) < 2 {
		return "", errors.New("choice dialog requires at least two options")
	}
	buttons := make([]string, 0, len(choices))
	for _, choice := range choices {
		if strings.TrimSpace(choice) == "" {
			return "", errors.New("choice dialog options cannot be empty")
		}
		buttons = append(buttons, `"`+appleScriptString(choice)+`"`)
	}
	script := fmt.Sprintf(`set apfsctlChoice to ""
try
 set apfsctlChoice to button returned of (display dialog "%s" with title "%s" buttons {%s} default button "%s")
on error number -128
 set apfsctlChoice to "__APFSCTL_DIALOG_CANCELED__"
end try
apfsctlChoice`, appleScriptString(message), appleScriptString(title), strings.Join(buttons, ", "), appleScriptString(choices[0]))
	cmd, err := appleScriptCommand(ctx, script)
	if err != nil {
		return "", err
	}
	output, err := cmd.Output()
	if err != nil {
		return "", errors.New("choice dialog is unavailable; use a logged-in GUI session")
	}
	choice := strings.TrimSpace(string(output))
	if choice == "__APFSCTL_DIALOG_CANCELED__" {
		return "", ErrDialogCanceled
	}
	for _, valid := range choices {
		if choice == valid {
			return choice, nil
		}
	}
	return "", errors.New("choice dialog returned an unexpected option")
}

func (AppleScriptPassphraseDialog) ShowMessage(ctx context.Context, title, message string) error {
	title = appleScriptString(title)
	message = appleScriptTextExpression(message)
	script := fmt.Sprintf(`set apfsctlResult to ""
try
 set apfsctlResult to button returned of (display dialog (%s) with title "%s" buttons {"OK"} default button "OK")
on error number -128
 set apfsctlResult to ""
end try
apfsctlResult`, message, title)
	cmd, err := appleScriptCommand(ctx, script)
	if err != nil {
		return err
	}
	if err := cmd.Run(); err != nil {
		return errors.New("result dialog is unavailable")
	}
	return nil
}

type ConfirmingPassphraseSource struct {
	Dialog AppleScriptPassphraseDialog
}

func (s ConfirmingPassphraseSource) ReadPassphrase(ctx context.Context, prompt string) ([]byte, error) {
	first, err := s.Dialog.ReadPassphrase(ctx, prompt)
	if err != nil {
		return nil, err
	}
	second, err := s.Dialog.ReadPassphrase(ctx, "Confirm the APFS volume passphrase:")
	if err != nil {
		clear(first)
		return nil, err
	}
	defer clear(second)
	if !bytes.Equal(first, second) {
		clear(first)
		return nil, errors.New("passphrases did not match; no volume was created")
	}
	return first, nil
}

func runAppleScriptPrompt(ctx context.Context, prompt string, hidden bool) ([]byte, error) {
	label := appleScriptString(prompt)
	answerMode := ""
	if hidden {
		answerMode = " with hidden answer"
	}
	script := fmt.Sprintf(`set apfsctlAnswer to ""
try
 set apfsctlAnswer to text returned of (display dialog "%s" default answer ""%s buttons {"Cancel", "Continue"} default button "Continue")
on error number -128
 set apfsctlAnswer to "__APFSCTL_DIALOG_CANCELED__"
end try
apfsctlAnswer`, label, answerMode)
	cmd, err := appleScriptCommand(ctx, script)
	if err != nil {
		return nil, err
	}
	output, err := cmd.Output()
	if err != nil {
		clear(output)
		return nil, errors.New("input dialog was canceled or unavailable; use a logged-in GUI session or provide input on stdin")
	}
	defer clear(output)
	answer := bytes.TrimSuffix(output, []byte("\n"))
	answer = bytes.TrimSuffix(answer, []byte("\r"))
	if bytes.Equal(answer, []byte("__APFSCTL_DIALOG_CANCELED__")) {
		return nil, ErrDialogCanceled
	}
	if len(answer) == 0 {
		return nil, errors.New("dialog input cannot be empty")
	}
	return append([]byte(nil), answer...), nil
}

func appleScriptCommand(ctx context.Context, script string) (*exec.Cmd, error) {
	const osascript = "/usr/bin/osascript"
	command, args := osascript, []string{"-e", script}
	if uid := os.Getenv("SUDO_UID"); uid != "" {
		if _, err := strconv.ParseUint(uid, 10, 32); err != nil {
			return nil, errors.New("invalid SUDO_UID for GUI dialog")
		}
		command = "/bin/launchctl"
		args = append([]string{"asuser", uid, osascript}, args...)
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stderr = os.Stderr
	return cmd, nil
}

func appleScriptString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}

func appleScriptTextExpression(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = `"` + appleScriptString(line) + `"`
	}
	return strings.Join(lines, " & return & ")
}

func readPassphrase(input *os.File, output io.Writer, prompt string) ([]byte, error) {
	if input == nil {
		return nil, errNoPassphraseOnStdin
	}
	if term.IsTerminal(int(input.Fd())) {
		if _, err := fmt.Fprint(output, prompt); err != nil {
			return nil, fmt.Errorf("write passphrase prompt: %w", err)
		}
		secret, err := term.ReadPassword(int(input.Fd()))
		_, printErr := fmt.Fprintln(output)
		if err != nil {
			return nil, fmt.Errorf("read passphrase from terminal: %w", err)
		}
		if printErr != nil {
			clear(secret)
			return nil, fmt.Errorf("finish passphrase prompt: %w", printErr)
		}
		if len(secret) == 0 {
			return nil, errors.New("passphrase cannot be empty")
		}
		return secret, nil
	}

	secret, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("read passphrase from stdin: %w", err)
	}
	if len(secret) > 0 && secret[len(secret)-1] == '\n' {
		secret[len(secret)-1] = 0
		secret = secret[:len(secret)-1]
	}
	if len(secret) > 0 && secret[len(secret)-1] == '\r' {
		secret[len(secret)-1] = 0
		secret = secret[:len(secret)-1]
	}
	if len(secret) == 0 {
		return nil, errNoPassphraseOnStdin
	}
	return secret, nil
}
