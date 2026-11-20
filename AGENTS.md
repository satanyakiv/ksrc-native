# AGENTS.md

Guidance for agents and people who change this repository.

## Context

`ksrc-native` is a Go command-line tool. It searches and reads three sources
that a Gradle dependency search does not reach:

- `kn.platform:<Framework>`: Kotlin signatures of Kotlin/Native platform klibs.
  The tool runs `klib dump-metadata`, splits the dump into one file per class
  and caches it.
- `kn.stdlib:native`: the native stdlib sources, extracted from a zip in the
  distribution.
- `sdk.apple:<Framework>`: Objective-C headers of an Xcode SDK, read in place.

The same operations are served as MCP tools by `ksrc-native mcp`. The project
follows the conventions of `respawn-llc/ksrc` and shares no code with it. Do
not copy files from that repository.

## Commands

```bash
scripts/build.sh                 # bin/ksrc-native, version from VERSION
gofmt -l .                       # must print nothing
go vet ./...
go test -count=1 ./...           # all tests
go test -short ./...             # without the tests that build the binary or run the real klib tool
go test -race ./internal/mcpserver ./internal/cli ./internal/adapter
scripts/package.sh dist          # release archives and checksums.txt
shellcheck scripts/*.sh
actionlint .github/workflows/*.yml
claude plugin validate --strict .claude-plugin/plugin.json
claude plugin validate --strict .claude-plugin/marketplace.json
```

Run `gofmt`, `go vet` and `go test` before every commit.

## Testing

- Tests need `rg` on `PATH`. Without it they skip; with `CI` set they fail.
- Tests need no Kotlin/Native distribution, no JDK and no Xcode.
  `internal/testutil` builds a fake distribution whose `klib` tool is the bash
  script `testdata/fake-klib/klib`, and a fake Xcode with fake `xcrun` and
  `xcode-select`. Only `rg` and that script run as processes.
- `testdata/dumps/usernotifications.txt` is a real excerpt of a metadata dump.
  `testdata/golden/usernotifications/` is what the splitter must produce from
  it. Change the golden files only when the output format changes on purpose.
- `TestRealDistribution` in `internal/cli` runs against `~/.konan` and the
  real Xcode. It skips itself without them and with `-short`.
- `cmd/ksrc-native` has tests that build the binary: exit codes of every kind
  of failure, and an MCP session over stdio.
- A new command or flag gets a test in `internal/adapter` (behaviour) and one
  in `internal/cli` (the command line). A new error path gets a case that
  checks the code, the hint and the exit code.

## Package map

| Package | Role |
| --- | --- |
| `cmd/ksrc-native` | `main` only: `os.Exit(cli.Main())` |
| `internal/cli` | cobra commands and flags; turns an error into a line on stderr and exit code 1 |
| `internal/adapter` | the operations: `Search`, `Cat`, `Deps`, `Where`, `Doctor`, `Clean`. The command line and the MCP server both call it |
| `internal/mcpserver` | MCP tools over the adapter; input structs, result text, the cap of one result (64 KiB, `--max-result-bytes`) |
| `internal/provider` | coordinates and the three providers (`Platform`, `Stdlib`, `AppleSDK`) |
| `internal/konan` | finds Kotlin/Native distributions, their platform klibs and the `klib` tool |
| `internal/xcode` | finds Xcode SDKs and framework headers through `xcrun` and `xcode-select` |
| `internal/dump` | splits a klib metadata dump into one file per class |
| `internal/search` | runs `rg --json` and formats the output lines |
| `internal/cat` | line ranges and file printing |
| `internal/cache` | cache root, atomic entries, usage and removal |
| `internal/errs` | coded errors: `E_CODE: message. Try: hint` |
| `internal/executil` | the `Runner` interface for running programs, with the real implementation |
| `internal/testutil` | fake distribution, fake Xcode, scripted runners |

Rules that follow from the map:

- New behaviour goes into `internal/adapter`. `internal/cli` and
  `internal/mcpserver` only translate inputs and outputs.
- Programs are run through `executil.Runner`, never through `os/exec`
  directly, so tests can replace them. Inside the adapter the runner is
  `a.runner()`, never `a.Runner`: it gives every program a deadline
  (`internal/adapter/limits.go`) and turns a stopped run into `E_TIMEOUT` or
  `E_CANCELLED`. Code that wraps the error of a run checks `errs.Stopped`
  first and passes such an error on unchanged.
- Nothing reads the environment, the home directory or the terminal directly.
  They come from the `Adapter` fields; only `adapter.New` wires those to the
  real ones.
- A value from the `--` tail of the command line or from an MCP input is
  untrusted: a model fills it, and text in a repository can steer the model.
  It never reaches a program as an argument and never becomes part of a path
  without a check. rg arguments pass `search.NormalizeExtraArgs` (an
  allow-list); a new rg flag is added there, with a test, or not at all. A
  target is compared with the names in `klib/platform`
  (`konan.Distribution.RequireTarget`), an SDK with the form of a name
  (`xcode.ValidSDKName`); the adapter runs both before a provider exists.
  The distribution (`--konan-home`) is never an MCP input.

## Code style

- Go, formatted by `gofmt`. No dependency is added without a reason written in
  the commit message.
- English in code, comments, documentation and commit messages.
- Every failure the user can see is an `errs` error with a code and a hint
  that says what to do next. Results go to stdout, everything else to stderr.
- Comments say why, or state a contract. They do not repeat the code.
- Table-driven tests with a name per case. A test message names what was
  called, what came back and what was expected.
- Documentation is plain prose: short sentences, no marketing words.
- Commits follow Conventional Commits: `feat(cli): ...`, `fix(dump): ...`,
  `docs: ...`, `test: ...`, `chore: ...`.

## Docs to keep in sync

A change to a command, a flag, an output line, an error code or an MCP input
touches all of these in the same commit:

| Change | Update |
| --- | --- |
| command, flag, output, error code | the `Long` and `Example` text in `internal/cli/cli.go`, `docs/cli-api.md`, `README.md`, `skills/ksrc-native/SKILL.md` |
| MCP tool or input | `internal/mcpserver/tools.go`, the MCP section of `docs/cli-api.md` and of the skill |
| a design choice | a dated entry in `docs/decisions.md` |
| version | `VERSION` and the manifests listed in `docs/release-workflow.md` |
| README examples | run the command and paste its real output, shortened with `...` |

## Things not to do

- Do not build all platform dumps in a code path the user did not ask for.
- Do not write outside the cache directory. `clean` removes only the tool's
  own cache directories.
- Do not print to stdout from the MCP server outside the protocol.
- Do not push, tag or publish a release without the maintainer's word.
