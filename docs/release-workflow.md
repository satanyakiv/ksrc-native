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
5. On GitHub, open Actions, pick `release`, and run it on `main`. A run on
   another branch fails in its first step with a message and tags nothing.
6. The workflow reads `VERSION`, stops if the tag exists, installs ripgrep,
   runs `go test -count=1 ./...`, builds the archives, pushes the tag
   `v<version>` and opens a draft release with generated notes. The tag is
   pushed only after the tests and the build passed.
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
dist/ksrc-native_<version>_<os>_<arch>.tar.gz    ksrc-native, LICENSE.txt, README.md,
                                                 THIRD_PARTY_NOTICES
dist/checksums.txt                               sha256 of every archive
```

## Third-party notices

The binary links the Go standard library and a few modules under
BSD-3-Clause, MIT and Apache-2.0 licences. These licences ask for their text
next to a binary, so every archive holds `THIRD_PARTY_NOTICES`.

`scripts/notices.sh` writes that file. It asks `go list -deps` which modules
`./cmd/ksrc-native` links for each release target, and copies the `LICENSE*`,
`COPYING*`, `NOTICE*` and `PATENTS*` files of each module from the local
module cache, and `LICENSE` and `PATENTS` of the Go installation. It
downloads nothing and stops when a module has no licence file.

The file is committed. After a dependency is added, removed or raised, run
`scripts/notices.sh` and commit the result. Three things fail when the file
is stale: the test `TestThirdPartyNoticesAreCurrent`, which the `ci` and
`release` workflows run, `scripts/notices.sh --check`, and
`scripts/package.sh`. The section of the Go standard library has no version
in it. If a new Go release changes its licence text, the check fails until
the file is written again.

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

## What guards a release

- **Only `main`.** The first step of the workflow fails when the run was
  started on another branch. It is a failing step, not a skipped job: a
  skipped job looks like a release that worked.
- **The whole test suite before the tag.** The workflow used to run only the
  manifest test. It now runs every test, with ripgrep installed. `CI` is set
  on a runner, so a missing `rg` fails the tests instead of skipping them.
- **Pinned actions.** Both workflows name `actions/checkout` and
  `actions/setup-go` by commit, with the version in a comment
  (`v4.4.0` and `v5.6.0`, resolved on 2026-11-20). A tag of an action can be
  moved to other code; a commit cannot. To update one, look up the commit of
  the new tag and change the hash and the comment together.
- **An installer that cannot run half of itself.** `scripts/install.sh` is
  meant for `curl ... | sh`. Its body is the function `main`, called on the
  last line. When the download stops halfway the shell has an unfinished
  function and no call, so it runs nothing. The test
  `TestInstallScriptCutShortRunsNothing` cuts the script after every line
  and checks that.

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
