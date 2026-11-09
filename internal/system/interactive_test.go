package system

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

type passphraseSourceStub struct {
	calls  int
	secret []byte
}

func (s *passphraseSourceStub) ReadPassphrase(context.Context, string) ([]byte, error) {
	s.calls++
	return s.secret, nil
}

func TestTerminalPassphraseSourceUsesDialogWithoutTerminalInput(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	defer input.Close()

	dialog := &passphraseSourceStub{secret: []byte("private phrase")}
	source := TerminalPassphraseSource{Input: input, Output: io.Discard, Dialog: dialog}
	got, err := source.ReadPassphrase(context.Background(), "Passphrase: ")
	if err != nil {
		t.Fatal(err)
	}
	defer clear(got)
	if string(got) != "private phrase" || dialog.calls != 1 {
		t.Fatalf("expected one dialog prompt and its secret; got %d calls and %q", dialog.calls, got)
	}
}

func TestCredentialSessionReusesAndClearsPassphrase(t *testing.T) {
	source := &passphraseSourceStub{secret: []byte("private phrase")}
	session := NewCredentialSession(source)

	for i := 0; i < 2; i++ {
		if err := session.RunWithPassphrase(context.Background(), "Passphrase: ", "/missing-test-command"); err == nil {
			t.Fatal("expected missing command to fail")
		}
	}
	if source.calls != 1 {
		t.Fatalf("expected one passphrase prompt, got %d", source.calls)
	}

	session.Close()
	if !bytes.Equal(source.secret, make([]byte, len(source.secret))) {
		t.Fatal("session did not clear the passphrase")
	}
}

func TestReadPassphraseFromPipe(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, " secret with spaces \n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	defer input.Close()

	var output bytes.Buffer
	got, err := readPassphrase(input, &output, "Passphrase: ")
	if err != nil {
		t.Fatal(err)
	}
	defer clear(got)
	if string(got) != " secret with spaces " {
		t.Fatalf("unexpected passphrase bytes: %q", got)
	}
	if output.Len() != 0 {
		t.Fatalf("non-interactive read wrote a prompt: %q", output.String())
	}
}

func TestReadPassphraseFromEmptyPipeFailsClearly(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	defer input.Close()

	_, err = readPassphrase(input, io.Discard, "Passphrase: ")
	if err == nil || err != errNoPassphraseOnStdin {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppleScriptStringEscapesDialogText(t *testing.T) {
	got := appleScriptString("Volume \\\"Proyectos\\\"\nready")
	want := `Volume \\\"Proyectos\\\" ready`
	if got != want {
		t.Fatalf("unexpected AppleScript string %q", got)
	}
}
