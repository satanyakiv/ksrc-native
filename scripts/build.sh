#!/usr/bin/env bash
# Builds bin/ksrc-native for the host platform.
# The version comes from the VERSION file and is injected into internal/cli.Version.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

version="$(tr -d '[:space:]' < VERSION)"
if [ -z "$version" ]; then
  echo "build.sh: VERSION file is empty" >&2
  exit 1
fi

module="github.com/satanyakiv/ksrc-native"
out="${1:-bin/ksrc-native}"

mkdir -p "$(dirname "$out")"
CGO_ENABLED=0 go build \
  -trimpath \
  -ldflags "-s -w -X ${module}/internal/cli.Version=${version}" \
  -o "$out" \
  ./cmd/ksrc-native

echo "built $out ($version)"
