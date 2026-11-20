#!/bin/sh
# Installs a released ksrc-native binary for this machine.
#
#   curl -fsSL https://raw.githubusercontent.com/satanyakiv/ksrc-native/main/scripts/install.sh | sh
#
# Variables:
#   KSRC_NATIVE_VERSION   version to install, for example 0.1.0 (default: the latest release)
#   KSRC_NATIVE_PREFIX    install prefix; the binary goes to <prefix>/bin (default: $HOME/.local)
#   KSRC_NATIVE_BASE_URL  where the release files are, for a mirror or a test
#                         (default: the GitHub release of the version)
#
# The script needs curl, tar and sha256sum or shasum. It checks the archive
# against checksums.txt of the release before it installs anything.
#
# Everything is inside main, and the last line calls it. A shell that reads
# the script from a pipe runs each command as it arrives; when the download
# stops halfway, it has an unfinished function and no call, and runs nothing.
set -eu

fail() {
  echo "install.sh: $*" >&2
  exit 1
}

need() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required but not installed"
}

main() {
  repo="satanyakiv/ksrc-native"
  prefix="${KSRC_NATIVE_PREFIX:-$HOME/.local}"
  version="${KSRC_NATIVE_VERSION:-}"

  need curl
  need tar

  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    *) fail "unsupported system $(uname -s); releases exist for macOS and Linux" ;;
  esac

  case "$(uname -m)" in
    arm64 | aarch64) arch="arm64" ;;
    x86_64 | amd64) arch="amd64" ;;
    *) fail "unsupported architecture $(uname -m); releases exist for arm64 and amd64" ;;
  esac

  if [ -z "$version" ]; then
    # The "latest" page redirects to the tag of the newest release.
    latest="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")" ||
      fail "cannot find the latest release of $repo; set KSRC_NATIVE_VERSION"
    version="${latest##*/v}"
    case "$version" in
      "" | *[!0-9A-Za-z.+-]*) fail "cannot read a version from $latest; set KSRC_NATIVE_VERSION" ;;
    esac
  fi
  version="${version#v}"

  base="${KSRC_NATIVE_BASE_URL:-https://github.com/$repo/releases/download/v$version}"
  archive="ksrc-native_${version}_${os}_${arch}.tar.gz"

  work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT

  echo "downloading $archive"
  curl -fsSL -o "$work/$archive" "$base/$archive" ||
    fail "cannot download $base/$archive"
  curl -fsSL -o "$work/checksums.txt" "$base/checksums.txt" ||
    fail "cannot download $base/checksums.txt"

  want="$(awk -v name="$archive" '$2 == name || $2 == "*" name { print $1 }' "$work/checksums.txt")"
  [ -n "$want" ] || fail "checksums.txt has no entry for $archive"
  if command -v sha256sum >/dev/null 2>&1; then
    got="$(sha256sum "$work/$archive" | awk '{ print $1 }')"
  elif command -v shasum >/dev/null 2>&1; then
    got="$(shasum -a 256 "$work/$archive" | awk '{ print $1 }')"
  else
    fail "sha256sum or shasum is required to check the download"
  fi
  [ "$got" = "$want" ] || fail "checksum mismatch for $archive: got $got, want $want"

  tar -C "$work" -xzf "$work/$archive" ksrc-native
  mkdir -p "$prefix/bin"
  # Write next to the target and rename, so a running binary is never half written.
  cp "$work/ksrc-native" "$prefix/bin/.ksrc-native.new"
  chmod 755 "$prefix/bin/.ksrc-native.new"
  mv "$prefix/bin/.ksrc-native.new" "$prefix/bin/ksrc-native"

  echo "installed ksrc-native $version to $prefix/bin/ksrc-native"
  case ":$PATH:" in
    *":$prefix/bin:"*) ;;
    *) echo "note: $prefix/bin is not on PATH; add it to your shell profile" ;;
  esac
}

main "$@"
