#!/bin/bash
# Install the apfsctl binary from a published GitHub release.
# Usage: curl -fsSL https://raw.githubusercontent.com/jmeiracorbal/apfsctl/main/install.sh | bash
# Pin a release with: APFSCTL_VERSION=v0.2.0-beta.3 bash

set -euo pipefail

REPO="jmeiracorbal/apfsctl"
INSTALL_DIR="${APFSCTL_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${APFSCTL_VERSION:-}"

fail() {
  printf 'apfsctl installer: %s\n' "$*" >&2
  exit 1
}

case "$(uname -s)" in
  Darwin) ;;
  *) fail "unsupported operating system: $(uname -s); apfsctl requires macOS" ;;
esac

case "$(uname -m)" in
  arm64|aarch64) ARCH="arm64" ;;
  x86_64|amd64) ARCH="amd64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v shasum >/dev/null 2>&1 || fail "shasum is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"
command -v install >/dev/null 2>&1 || fail "install is required"

TMP_DIR="$(mktemp -d)"
STAGED=""
cleanup() {
  rm -rf "$TMP_DIR"
  if [ -n "$STAGED" ]; then
    rm -f "$STAGED"
  fi
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if [ -z "$VERSION" ]; then
  curl -fsSL "https://api.github.com/repos/${REPO}/releases?per_page=1" -o "$TMP_DIR/releases.json" ||
    fail "could not fetch published releases"
  VERSION="$(awk -F '"' '/"tag_name"[[:space:]]*:/ { print $4; exit }' "$TMP_DIR/releases.json")"
  [ -n "$VERSION" ] || fail "could not determine the latest release"
fi

case "$VERSION" in
  v[0-9]*) ;;
  *) fail "invalid release tag: ${VERSION}" ;;
esac
case "$VERSION" in
  *[!A-Za-z0-9.+-]*) fail "invalid release tag: ${VERSION}" ;;
esac

ARCHIVE="apfsctl_${VERSION}_darwin_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
ARCHIVE_PATH="$TMP_DIR/$ARCHIVE"
CHECKSUMS_PATH="$TMP_DIR/checksums.txt"

printf 'Installing apfsctl %s for darwin/%s...\n' "$VERSION" "$ARCH"
curl -fsSL "${BASE_URL}/${ARCHIVE}" -o "$ARCHIVE_PATH" || fail "download failed: ${ARCHIVE}"
curl -fsSL "${BASE_URL}/checksums.txt" -o "$CHECKSUMS_PATH" || fail "could not download release checksums"

EXPECTED="$(awk -v file="./${ARCHIVE}" '$2 == file { print $1; exit }' "$CHECKSUMS_PATH")"
[ -n "$EXPECTED" ] || fail "no checksum found for ${ARCHIVE}"
ACTUAL="$(shasum -a 256 "$ARCHIVE_PATH" | awk '{ print $1 }')"
[ "$EXPECTED" = "$ACTUAL" ] || fail "checksum mismatch for ${ARCHIVE}"

tar -tzf "$ARCHIVE_PATH" > "$TMP_DIR/archive-contents.txt" || fail "invalid release archive"
[ "$(cat "$TMP_DIR/archive-contents.txt")" = "apfsctl" ] || fail "unexpected contents in release archive"
tar -xzf "$ARCHIVE_PATH" -C "$TMP_DIR" || fail "could not extract release archive"
[ -f "$TMP_DIR/apfsctl" ] && [ ! -L "$TMP_DIR/apfsctl" ] || fail "release archive does not contain the apfsctl executable"

mkdir -p "$INSTALL_DIR" || fail "could not create install directory: ${INSTALL_DIR}"
STAGED="${INSTALL_DIR}/.apfsctl.$$"
install -m 0755 "$TMP_DIR/apfsctl" "$STAGED" || fail "could not stage apfsctl in ${INSTALL_DIR}"
mv -f "$STAGED" "${INSTALL_DIR}/apfsctl" || fail "could not install apfsctl in ${INSTALL_DIR}"
STAGED=""

printf 'Installed: %s/apfsctl\n' "$INSTALL_DIR"
"${INSTALL_DIR}/apfsctl" version
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) printf 'Add this to your shell profile if needed: export PATH="%s:$PATH"\n' "$INSTALL_DIR" ;;
esac
