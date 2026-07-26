#!/bin/sh
set -eu

REPOSITORY="gustmrg/timesheet-cli"
BINARY="timesheet"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) fail "unsupported operating system: $os (supported: darwin, linux)" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported architecture: $(uname -m) (supported: amd64, arm64)" ;;
esac

if [ "$VERSION" = "latest" ]; then
  release_url="https://github.com/$REPOSITORY/releases/latest"
  resolved_url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "$release_url") || fail "could not determine the latest release"
  VERSION=$(printf '%s' "$resolved_url" | sed 's#^.*/tag/v##')
  [ -n "$VERSION" ] || fail "could not determine the latest release version"
else
  VERSION=${VERSION#v}
fi

archive="${REPOSITORY##*/}_${VERSION}_${os}_${arch}.tar.gz"
base_url="https://github.com/$REPOSITORY/releases/download/v$VERSION"
tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t timesheet-install)
trap 'rm -rf "$tmp_dir"' EXIT

printf 'Downloading timesheet-cli v%s for %s/%s...\n' "$VERSION" "$os" "$arch"
curl -fsSL "$base_url/$archive" -o "$tmp_dir/$archive" || fail "could not download release archive"
curl -fsSL "$base_url/checksums.txt" -o "$tmp_dir/checksums.txt" || fail "could not download release checksums"

expected=$(awk -v file="$archive" '$2 == file { print $1; exit }' "$tmp_dir/checksums.txt")
[ -n "$expected" ] || fail "archive checksum was not found"

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp_dir/$archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$tmp_dir/$archive" | awk '{print $1}')
else
  fail "sha256sum or shasum is required to verify the download"
fi
[ "$actual" = "$expected" ] || fail "checksum verification failed"

tar -xzf "$tmp_dir/$archive" -C "$tmp_dir" || fail "could not extract release archive"
[ -f "$tmp_dir/$BINARY" ] || fail "release archive did not contain $BINARY"

if [ ! -d "$INSTALL_DIR" ]; then
  mkdir -p "$INSTALL_DIR" 2>/dev/null || true
fi
if [ ! -w "$INSTALL_DIR" ]; then
  if command -v sudo >/dev/null 2>&1; then
    printf 'Installing to %s (sudo required)...\n' "$INSTALL_DIR"
    sudo install -m 0755 "$tmp_dir/$BINARY" "$INSTALL_DIR/$BINARY"
  else
    fail "$INSTALL_DIR is not writable and sudo is unavailable; set INSTALL_DIR to a writable directory"
  fi
else
  install -m 0755 "$tmp_dir/$BINARY" "$INSTALL_DIR/$BINARY"
fi

printf 'Installed timesheet v%s to %s\n' "$VERSION" "$INSTALL_DIR/$BINARY"
case ":${PATH}:" in
  *:"$INSTALL_DIR":*) ;;
  *) printf 'warning: %s is not on PATH\n' "$INSTALL_DIR" >&2 ;;
esac
