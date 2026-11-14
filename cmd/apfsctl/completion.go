package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jmeiracorbal/apfsctl/internal/apfs"
	"github.com/jmeiracorbal/apfsctl/internal/managed"
	"github.com/jmeiracorbal/apfsctl/internal/projectmove"
	"github.com/jmeiracorbal/apfsctl/internal/system"
)

const zshCompletion = `#compdef apfsctl
_apfsctl() {
  local -a commands targets
  if (( CURRENT == 2 )); then
    commands=(
      'move:Move a project to a managed APFS volume'
      'setup:Guided APFS setup'
      'status:Inspect APFS volumes'
      'doctor:Diagnose APFS configuration'
      'plan:Inspect proposed changes'
      'create:Create or reconcile a volume'
      'mount:Mount an existing volume'
      'configure:Manage APFS policies'
      'version:Show version'
    )
    _describe 'apfsctl command' commands
    return
  fi
  if [[ "$words[2]" == move ]]; then
    if (( CURRENT == 3 )); then
      _files -/
    elif (( CURRENT == 4 )); then
      targets=("${(@f)$(command apfsctl completion targets 2>/dev/null)}")
      _describe 'managed APFS volume' targets
    fi
  fi
}
compdef _apfsctl apfsctl
`

func runCompletion(ctx context.Context, args []string, client apfs.Client, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: apfsctl completion zsh|targets|refresh")
	}
	switch args[0] {
	case "zsh":
		_, err := io.WriteString(stdout, zshCompletion)
		return err
	case "targets":
		records, err := (managed.Store{Path: managed.RegistryPath}).Read()
		if err != nil { return err }
		snapshot, err := client.Snapshot(ctx)
		if err != nil { return err }
		volumes, err := managed.Available(records, snapshot)
		if err != nil { return err }
		counts := map[string]int{}
		for _, volume := range volumes { counts[volume.Name]++ }
		for _, volume := range volumes {
			if counts[volume.Name] == 1 {
				if _, err := fmt.Fprintln(stdout, volume.Name); err != nil { return err }
			}
		}
		return nil
	case "refresh":
		if os.Geteuid() != 0 {
			return system.ReexecWithSudo(ctx, []string{"completion","refresh"}, stdout, stderr)
		}
		if err := managed.Refresh(ctx, client.Snapshot); err != nil { return err }
		_, err := fmt.Fprintln(stdout,"Managed APFS volume registry refreshed.")
		return err
	default:
		return fmt.Errorf("unknown completion command %q", args[0])
	}
}

func runMove(ctx context.Context, args []string, client apfs.Client, stdout io.Writer) error {
	if len(args) != 2 {
		return errors.New("usage: apfsctl move <source-directory> <managed-volume-name>")
	}
	records, err := (managed.Store{Path: managed.RegistryPath}).Read()
	if err != nil { return err }
	service := projectmove.Service{Snapshot: client.Snapshot, Copy: projectmove.Ditto{}}
	result, err := service.Move(ctx, args[0], strings.TrimSpace(args[1]), records)
	if err != nil { return err }
	return printJSON(stdout, result)
}
