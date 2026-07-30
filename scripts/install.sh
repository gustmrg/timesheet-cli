#!/bin/sh
set -eu

REPOSITORY="gustmrg/timesheet-cli"
BINARY="timesheet"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
TOKEN="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
API_ROOT="https://api.github.com/repos/$REPOSITORY"

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

api_request() {
  accept=$1
  shift
  if [ -n "$TOKEN" ]; then
    curl -fsSL -H "Authorization: Bearer $TOKEN" -H "Accept: $accept" "$@"
  else
    curl -fsSL -H "Accept: $accept" "$@"
  fi
}

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

tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t timesheet-install)
trap 'rm -rf "$tmp_dir"' EXIT
release_json="$tmp_dir/release.json"
auth_hint=""
[ -n "$TOKEN" ] || auth_hint="; set GH_TOKEN if the repository is private"

if [ "$VERSION" = "latest" ]; then
  release_endpoint="$API_ROOT/releases/latest"
else
  VERSION=${VERSION#v}
  release_endpoint="$API_ROOT/releases/tags/v$VERSION"
fi

api_request "application/vnd.github+json" -o "$release_json" "$release_endpoint" || fail "could not retrieve release metadata$auth_hint"

if [ "$VERSION" = "latest" ]; then
  VERSION=$(awk -F '"' '/"tag_name":/ { tag=$4; sub(/^v/, "", tag); print tag; exit }' "$release_json")
  [ -n "$VERSION" ] || fail "could not determine the latest release version"
fi
case "$VERSION" in
  *[!0-9A-Za-z._-]*) fail "invalid release version: $VERSION" ;;
esac

asset_id() {
  awk -v file="$1" '
    index($0, "/releases/assets/") {
      id=$0
      sub(/^.*\/releases\/assets\//, "", id)
      sub(/[^0-9].*$/, "", id)
    }
    {
      line=$0
      gsub(/[[:space:]]/, "", line)
    }
    index(line, "\"name\":\"" file "\"") && id != "" { print id; exit }
  ' "$release_json"
}

download_asset() {
  name=$1
  destination=$2
  id=$(asset_id "$name")
  [ -n "$id" ] || fail "release asset was not found: $name"
  api_request "application/octet-stream" -o "$destination" "$API_ROOT/releases/assets/$id" || fail "could not download release asset: $name"
}

archive="${REPOSITORY##*/}_${VERSION}_${os}_${arch}.tar.gz"
printf 'Downloading timesheet-cli v%s for %s/%s...\n' "$VERSION" "$os" "$arch"
download_asset "$archive" "$tmp_dir/$archive"
download_asset "checksums.txt" "$tmp_dir/checksums.txt"

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
