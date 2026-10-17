#!/usr/bin/env bash
# Resolves the version a release dispatch asked for and checks it against the
# tags already published. Reads `git ls-remote --tags origin` on stdin.
#
#   release-version.sh patch|minor|major   bumps the latest release
#   release-version.sh beta                next minor version's next beta
#   release-version.sh 1.2.3[-rc.1]        an exact version
#
# Prints the version without the leading v, or fails with a GitHub Actions
# error annotation.
set -euo pipefail

fail() {
  echo "::error::$1" >&2
  exit 1
}

# Prints the higher of two dotted versions.
higher() { printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n 1; }

requested="${1:-}"
requested="${requested#v}"
[[ -n "$requested" ]] || fail "Pass patch, minor, major, beta, or an exact version such as 1.2.3."

tags="$(sed -nE 's#.*refs/tags/v([0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)\.[0-9]+)?)$#\1#p' | sort -uV)"
latest="$(grep -v -- - <<<"$tags" | tail -n 1 || true)"
latest="${latest:-0.0.0}"
IFS=. read -r major minor patch <<<"$latest"

case "$requested" in
  patch) version="$major.$minor.$((patch + 1))" ;;
  minor) version="$major.$((minor + 1)).0" ;;
  major) version="$((major + 1)).0.0" ;;
  beta)
    next="$major.$((minor + 1)).0"
    last="$(grep -E "^${next//./\\.}-beta\.[0-9]+$" <<<"$tags" | sed -E 's/.*-beta\.//' | sort -n | tail -n 1 || true)"
    version="$next-beta.$((${last:-0} + 1))"
    ;;
  *) version="$requested" ;;
esac

if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)\.[0-9]+)?$ ]]; then
  fail "$requested is not patch, minor, major, beta, or a version such as 1.2.3 or 1.3.0-rc.1."
fi
if grep -qxF -- "$version" <<<"$tags"; then
  fail "Tag v$version already exists."
fi

# `timesheet upgrade` and the installers follow the latest release, so a new
# release must sort above it. A prerelease is a preview of a later release.
base="${version%%-*}"
if [[ "$base" == "$latest" || "$(higher "$base" "$latest")" != "$base" ]]; then
  if [[ "$version" == *-* ]]; then
    fail "v$latest is already out; a prerelease must preview a later version, such as $major.$((minor + 1)).0-rc.1."
  fi
  fail "v$latest is already out; release a higher version than v$version."
fi
if [[ "$version" == *-* ]]; then
  newest="$(grep -E "^${base//./\\.}-" <<<"$tags" | tail -n 1 || true)"
  if [[ -n "$newest" && "$(higher "$version" "$newest")" != "$version" ]]; then
    fail "v$newest is already out; the next prerelease of $base must sort above it."
  fi
fi

echo "$version"
