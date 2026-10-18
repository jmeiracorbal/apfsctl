package main

import (
	"bytes"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"version"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != version+"\n" {
		t.Fatalf("unexpected version output: %q", got)
	}
}
