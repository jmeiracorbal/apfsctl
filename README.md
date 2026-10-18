# devvolume

![macOS](https://img.shields.io/badge/platform-macOS-222222)
![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8)
[![Beta](https://img.shields.io/badge/version-v0.1.0--beta.1-orange)](https://github.com/jmeiracorbal/verbose-octo-spork/releases/tag/v0.1.0-beta.1)

## Objective

Manage dedicated, encrypted APFS volumes for development repositories on macOS through an idempotent Go CLI. Changes are planned from the actual system state and applied only when necessary.

## Features

- Create, unlock, and mount encrypted APFS volumes.
- Inspect system state with `status`, `doctor`, and `plan`.
- Reconcile configuration without repeating completed operations; reject conflicts rather than overwrite existing data.
- Recover safely from interrupted operations using a private journal and an exclusive lock.
- Configure persistent mount entries, Spotlight, optional FSEvents suppression, and non-recursive volume ownership.
- Execute macOS utilities directly without generating Bash scripts.

## Requirements

- macOS with APFS; Apple Silicon or Intel.
- Administrator access for volume changes and an interactive terminal for encryption prompts.
- An existing, empty mount directory.
- Go 1.22+ only when building from source.

**Experimental beta:** Privileged APFS operations and reboot behavior still require testing on real hardware. Use a disposable volume and a backup. `fstab` does not automatically unlock encrypted volumes. `unmount` and `remove` are not yet implemented.

## Installation

Download the appropriate archive from [Releases](https://github.com/jmeiracorbal/verbose-octo-spork/releases/tag/v0.1.0-beta.1):

- Apple Silicon: `devvolume_v0.1.0-beta.1_darwin_arm64.tar.gz`
- Intel: `devvolume_v0.1.0-beta.1_darwin_amd64.tar.gz`

Extract and install it (Apple Silicon example; replace `arm64` with `amd64` on Intel):

```sh
tar -xzf devvolume_v0.1.0-beta.1_darwin_arm64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 0755 devvolume "$HOME/.local/bin/devvolume"
"$HOME/.local/bin/devvolume" version
```

Alternatively, build from source:

```sh
go build -o devvolume ./cmd/devvolume
```

## References

Inspired by [`repos-volume.sh` from elithrar/dotfiles](https://github.com/elithrar/dotfiles/blob/main/repos-volume.sh). devvolume is an independent Go implementation.
