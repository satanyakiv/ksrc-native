# Release workflow

A release is a git tag `v<version>`, four archives and a `checksums.txt` on a
GitHub release. The workflow `.github/workflows/release.yml` makes them. A
person starts it and publishes the result.

## Where the version lives

| File | Field |
| --- | --- |
| `VERSION` | the whole file |
| `.claude-plugin/plugin.json` | `version` |
| `.claude-plugin/marketplace.json` | `metadata.version` and `plugins[0].version` |
| `.codex-plugin/plugin.json` | `version` |

`VERSION` is the source. `scripts/build.sh` injects it into the binary. The
test `TestManifestsNameTheVersionOfTheVERSIONFile` fails when a manifest
differs.

## Steps

1. Raise the version in `VERSION` and in the three manifests. Use
   `major.minor.patch`.
2. Run the checks:

   ```bash
   gofmt -l .
   go vet ./...
   go test -count=1 ./...
   claude plugin validate --strict .claude-plugin/plugin.json
   claude plugin validate --strict .claude-plugin/marketplace.json
   ```

3. If a command, a flag, an output line or an error code changed, update
   `docs/cli-api.md`, `README.md` and `skills/ksrc-native/SKILL.md`. Run the
   README examples again and paste the real output.
4. Commit as `chore(release): <version>` and merge to `main`. Wait for the
   `ci` workflow to pass.
5. On GitHub, open Actions, pick `release`, and run it on `main`.
6. The workflow reads `VERSION`, stops if the tag exists, builds the archives,
   pushes the tag `v<version>` and opens a draft release with generated notes.
7. Read the draft. Edit the notes. Publish it.
8. Check an install from the release:

   ```bash
   KSRC_NATIVE_PREFIX="$(mktemp -d)" sh scripts/install.sh
   go install github.com/satanyakiv/ksrc-native/cmd/ksrc-native@latest
   ```

## What the workflow builds

`scripts/package.sh` cross-builds with the settings of `scripts/build.sh`
(`CGO_ENABLED=0`, `-trimpath`, the version injected) for `darwin/arm64`,
`darwin/amd64`, `linux/arm64` and `linux/amd64`:

```text
dist/ksrc-native_<version>_<os>_<arch>.tar.gz    ksrc-native, LICENSE.txt, README.md
dist/checksums.txt                               sha256 of every archive
```

`scripts/install.sh` downloads one archive and `checksums.txt` from the
release and refuses to install when the sha256 differs.

To try the packaging without a release:

```bash
scripts/package.sh /tmp/ksrc-native-dist
KSRC_NATIVE_VERSION="$(cat VERSION)" \
KSRC_NATIVE_BASE_URL="file:///tmp/ksrc-native-dist" \
KSRC_NATIVE_PREFIX=/tmp/ksrc-native-prefix \
  sh scripts/install.sh
```

## If something goes wrong

- **The workflow failed before the tag step.** Fix the cause and run it again.
  Nothing was published.
- **The tag exists but the release is wrong.** Delete the draft release and
  the tag on GitHub, fix, and run the workflow again. Do this only while the
  release is a draft.
- **A published release is broken.** Do not replace its files: `go install`
  and the checksums of earlier downloads would disagree. Raise the patch
  version and release again.

## Notes

- The binaries are not signed or notarized. A binary downloaded by `curl` or
  built by `go install` runs on macOS without a Gatekeeper prompt; one
  downloaded by a browser may need `xattr -d com.apple.quarantine`.
- While the repository is private, `go install`, `scripts/install.sh` and the
  plugin commands need access to it. They are meant for the public repository.
