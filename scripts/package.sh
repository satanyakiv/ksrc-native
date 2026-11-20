#!/usr/bin/env bash
# Cross-builds the release archives into dist/ (or the directory given as $1):
#
#   ksrc-native_<version>_<os>_<arch>.tar.gz   the binary, LICENSE.txt, README.md,
#                                              THIRD_PARTY_NOTICES
#   checksums.txt                              sha256 of every archive
#
# The build settings are those of scripts/build.sh. KSRC_NATIVE_TARGETS
# overrides the list of <os>/<arch> pairs.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

version="$(tr -d '[:space:]' < VERSION)"
dist="${1:-dist}"
targets="${KSRC_NATIVE_TARGETS:-darwin/arm64 darwin/amd64 linux/arm64 linux/amd64}"

# The binary carries code under BSD-3-Clause, MIT and Apache-2.0, and those
# licences ask for their text next to a binary. An archive with notices of
# another dependency set would be wrong, so a stale file stops the packaging.
scripts/notices.sh --check

mkdir -p "$dist"
dist="$(cd "$dist" && pwd)"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

archives=()
for target in $targets; do
  os="${target%/*}"
  arch="${target#*/}"
  name="ksrc-native_${version}_${os}_${arch}"
  mkdir -p "$stage/$name"
  GOOS="$os" GOARCH="$arch" scripts/build.sh "$stage/$name/ksrc-native"
  cp LICENSE.txt README.md THIRD_PARTY_NOTICES "$stage/$name/"
  # COPYFILE_DISABLE keeps macOS tar from adding AppleDouble files.
  COPYFILE_DISABLE=1 tar -C "$stage/$name" -czf "$dist/$name.tar.gz" ksrc-native LICENSE.txt README.md THIRD_PARTY_NOTICES
  archives+=("$name.tar.gz")
done

cd "$dist"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "${archives[@]}" > checksums.txt
else
  shasum -a 256 "${archives[@]}" > checksums.txt
fi

echo "packaged ${#archives[@]} archives for $version in $dist"
