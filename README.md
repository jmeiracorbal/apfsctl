# apfsctl

![macOS](https://img.shields.io/badge/platform-macOS-222222)
![Go](https://img.shields.io/badge/Go-1.22%2B-00ADD8)
[![Beta](https://img.shields.io/badge/version-v0.2.0--beta.5-orange)](https://github.com/jmeiracorbal/apfsctl/releases/tag/v0.2.0-beta.5)

## Objective

Manage dedicated, encrypted APFS volumes for development repositories on macOS through an idempotent Go CLI. Changes are planned from the actual system state and applied only when necessary.

## Features

- Create, unlock, and mount encrypted APFS volumes.
- Inspect system state with `status`, `doctor`, and `plan`.
- Reconcile configuration without repeating completed operations; reject conflicts rather than overwrite existing data.
- Recover safely from interrupted operations using a private journal and an exclusive lock.
- Configure persistent `rw,nobrowse,owners` mount entries, Spotlight, optional FSEvents suppression, and non-recursive volume ownership.
- Execute macOS utilities directly without generating Bash scripts.

## Requirements

- macOS with APFS; Apple Silicon or Intel.
- Administrator access for volume changes. Encryption prompts work in a terminal, a logged-in macOS GUI session, or with a passphrase supplied on stdin.
- An empty mount directory. If it does not exist, `apfsctl` creates it and protects it while the volume is unmounted.
- Go 1.22+ only when building from source.

## First run

Run the CLI without arguments to open the setup assistant:

```sh
sudo apfsctl
```

It asks for a volume name, detects the active macOS APFS container, creates a sanitized mount path under `/Volumes`, and asks for a new encryption passphrase twice. If that volume already exists and is locked, it asks for the existing passphrase; if it is already mounted, it reports that it is ready. Setup success and errors appear in dialogs instead of the console when the GUI is available. Advanced users can continue to use the explicit `create`, `mount`, `plan`, and `status` commands.

**Experimental beta:** Privileged APFS operations and reboot behavior still require testing on real hardware. Use a disposable volume and a backup. The mount point is left owned by `root:wheel` with mode `000` while the APFS volume is unmounted, preventing accidental writes to the underlying startup volume. Mounts use `rw,nobrowse,owners`; `nobrowse` hides the volume from normal Finder browsing but does not restrict access by path.

FSEvents suppression remains configurable: `configure --fsevents keep` preserves the current behavior, while `configure --fsevents disable` disables persistent FSEvents logging for the volume. This does not stop live filesystem notifications, and backup tools that rely on persisted events may need to rescan. Spotlight indexing can also be disabled per volume. `fstab` does not automatically unlock encrypted volumes, so an encrypted volume must be unlocked after restart. `unmount` and `remove` are not yet implemented.

When stdin is a terminal, `apfsctl` reads the passphrase without echoing it. If no terminal is available in a logged-in macOS GUI session, it opens a hidden-input dialog. Headless scripts can instead pipe the passphrase from a secret manager (for example, `secret-manager read apfs-volume | sudo -n apfsctl create ...`); the privileged command must already be authorized without consuming that stdin. Do not put passphrases in command arguments or environment variables.

During `create`, the passphrase stays in memory only long enough to create and unlock the new volume, then is cleared. A later `mount` of a locked volume requests the passphrase again.

## Installation

Install the latest published release:

```sh
curl -fsSL https://raw.githubusercontent.com/jmeiracorbal/apfsctl/main/install.sh | bash
```

To pin a release or choose another install directory:

```sh
curl -fsSL https://raw.githubusercontent.com/jmeiracorbal/apfsctl/main/install.sh | APFSCTL_VERSION=v0.2.0-beta.5 bash
curl -fsSL https://raw.githubusercontent.com/jmeiracorbal/apfsctl/main/install.sh | APFSCTL_INSTALL_DIR="$HOME/.local/bin" bash
```

The installer detects Apple Silicon or Intel, verifies the archive against the release checksum, and installs the CLI under `~/.local/bin` by default. It does not use `sudo` or create/configure APFS volumes.

For manual installation, download the appropriate archive from [Releases](https://github.com/jmeiracorbal/apfsctl/releases/tag/v0.2.0-beta.5):

- Apple Silicon: `apfsctl_v0.2.0-beta.5_darwin_arm64.tar.gz`
- Intel: `apfsctl_v0.2.0-beta.5_darwin_amd64.tar.gz`

Extract and install it (Apple Silicon example; replace `arm64` with `amd64` on Intel):

```sh
tar -xzf apfsctl_v0.2.0-beta.5_darwin_arm64.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 0755 apfsctl "$HOME/.local/bin/apfsctl"
"$HOME/.local/bin/apfsctl" version
```

Alternatively, build from source:

```sh
go build -o apfsctl ./cmd/apfsctl
```

## References

Inspired by [`repos-volume.sh` from elithrar/dotfiles](https://github.com/elithrar/dotfiles/blob/main/repos-volume.sh). apfsctl is an independent Go implementation.
