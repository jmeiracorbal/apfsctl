package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/jmeiracorbal/verbose-octo-spork/internal/apfs"
	"github.com/jmeiracorbal/verbose-octo-spork/internal/domain"
	"github.com/jmeiracorbal/verbose-octo-spork/internal/reconcile"
	"github.com/jmeiracorbal/verbose-octo-spork/internal/system"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: devvolume status | plan [flags] | doctor | create | mount | unmount | remove")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("devvolume requires macOS (APFS); unit tests support other platforms")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := apfs.Client{Run: system.ExecRunner{}}
	switch args[0] {
	case "status", "doctor":
		if len(args) != 1 {
			return errors.New("unexpected arguments")
		}
		s, err := client.Snapshot(ctx)
		if err != nil {
			return err
		}
		return printJSON(stdout, s)
	case "plan":
		flags := flag.NewFlagSet("plan", flag.ContinueOnError)
		flags.SetOutput(stderr)
		name := flags.String("name", "repos", "APFS volume name")
		container := flags.String("container", "", "APFS container UUID")
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		mount := flags.String("mount", filepath.Join(home, "repos"), "absolute mount directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return errors.New("unexpected positional arguments")
		}
		d := domain.Desired{Name: *name, ContainerUUID: *container, MountPoint: *mount, Encrypted: true}
		if err := d.Validate(); err != nil {
			return err
		}
		s, err := client.Snapshot(ctx)
		if err != nil {
			return err
		}
		p, err := reconcile.Build(d, s)
		if err != nil {
			return err
		}
		if !p.Noop {
			if err := ensureSafeMountTarget(d.MountPoint, p); err != nil {
				return err
			}
		}
		return printJSON(stdout, p)
	case "create", "mount", "unmount", "remove":
		return fmt.Errorf("%s is not implemented: privileged mutations are disabled in this skeleton", args[0])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func ensureSafeMountTarget(path string, p reconcile.Plan) error {
	needsMount := false
	for _, change := range p.Changes {
		if change.Action == reconcile.Mount {
			needsMount = true
		}
	}
	if !needsMount {
		return nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("mount point exists but is not a real directory")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("mount point %q is not empty; refusing to hide existing files", path)
	}
	return nil
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
