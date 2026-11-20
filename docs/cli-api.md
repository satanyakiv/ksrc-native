# Command line and MCP reference

This page mirrors `ksrc-native --help`. When a flag, an output line or an error
code changes in the code, change it here in the same commit.

## Modules

A module is named by a coordinate `group:name[:version]`.

| Coordinate | Content | Version part |
| --- | --- | --- |
| `kn.platform:<Framework>[:<kotlinVersion>]` | Kotlin signatures of `platform.<Framework>`, one file per class | Kotlin version of the distribution |
| `kn.stdlib:native[:<kotlinVersion>]` | sources of the native stdlib and `kotlinx.cinterop` | Kotlin version of the distribution |
| `sdk.apple:<Framework>[:<sdk>]` | Objective-C headers of an Xcode SDK | SDK name: `iphoneos`, `macosx`, `iphoneos26.4`, ... |

Framework names are matched without regard to case. Output always carries the
real name and the resolved version: `kn.platform:uikit` prints as
`kn.platform:UIKit:2.4.21`.

All `kn.*` modules of one command must name the same Kotlin version. The
version part of an `sdk.apple` coordinate is an SDK name, so it may differ.

## Global flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--konan-home <path>` | discovered | path of the Kotlin/Native distribution |
| `--kotlin-version <version>` | discovered | Kotlin version of the distribution to use, for example `2.4.21` |
| `--project <dir>` | `.` | project directory; the Kotlin version is read from its `gradle/libs.versions.toml` |
| `--target <target>` | `ios_arm64` | Kotlin/Native target of the platform libraries |
| `--sdk <sdk>` | `iphoneos` | Apple SDK of `sdk.apple` modules |
| `--version`, `-v` | | print `ksrc-native <version>` |
| `--help`, `-h` | | help for the tool or a command |

The distribution is picked in this order: `--konan-home`; the `KONAN_HOME`
variable; `--kotlin-version` or the version in a `kn.*` coordinate;
`kotlin = "..."` in `gradle/libs.versions.toml` of `--project`; the newest
distribution under `KONAN_DATA_DIR` or `~/.konan`. When the project's version
is not installed, a warning goes to stderr and the newest one is used.

An SDK named in an `sdk.apple` coordinate wins over `--sdk`.

## Commands

### `search <pattern> [--module <coordinate>]... [-- <rg-args>]`

Alias: `rg`. Runs ripgrep over the modules.

| Flag | Meaning |
| --- | --- |
| `--module`, `-m` | module to search; repeatable |
| `--all-platform` | build and search every platform library of the target |
| `--context`, `-C` | lines of context around each match |
| `-- <rg-args>` | everything after `--` goes to ripgrep, for example `-i`, `-F`, `-g '*Delegate*'` |

Without `--module`, the cached platform dumps of the target and the stdlib are
searched. Nothing is built. If neither exists, the command fails with
`E_NO_SOURCES`.

Output, one line per match:

```text
<file-id> <line>:<col>:<text>
```

A context line is `<file-id> <line>-<text>`. A line `--` separates groups of
lines that are not adjacent. Lines and columns start at 1; the column counts
bytes.

No match is a success: stdout is empty, stderr gets
`ksrc-native: no matches for "<pattern>" in <modules>`, and the exit code is 0.

### `cat <file-id> [--lines A,B] [-n]`

Alias: `open`. Prints the file a file-id names.

| Flag | Meaning |
| --- | --- |
| `--lines A,B` | line range, 1-based and inclusive; `A` alone prints one line; default: the whole file |
| `--line-numbers`, `-n` | prefix every line with its number and a tab |

A file-id is `group:name:version!/inner/path`:

```text
kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt
kn.stdlib:native:2.4.21!/nativeMain/kotlinx/cinterop/Utils.kt
sdk.apple:UIKit:iphoneos26.4!/UIViewController.h
```

For `kn.platform` the first path segment is the target, so `--target` is not
needed. `cat` builds a missing dump the same way `search` does.

### `deps`

Lists what can be searched, one module per line:

```text
kn.platform:<Framework>:<kotlinVersion> (<target>, dump cached: yes|no)
kn.stdlib:native:<kotlinVersion> (extracted: yes|no, <zip path>)
sdk.apple:<Framework>:<sdk> (<n> frameworks with headers, <sdk path>)
```

The `sdk.apple` line stands for all frameworks of one installed SDK;
`<Framework>` is literal there. A source that is not available is reported on
stderr as `ksrc-native: skipped: <error>` and left out. The command fails when
nothing can be listed, or when `--konan-home` or `--kotlin-version` names a
distribution that cannot be used.

### `where <coordinate>`

Prints one path: the klib directory of a platform library, the stdlib sources
zip, or the `Headers` directory of an SDK framework. It builds nothing.

### `doctor`

Prints one line per part: `rg`, `kotlin-native`, `klib`, `platform`, `stdlib`,
`xcode`, `cache`. Each line is `<part>: <status> - <details>`.

| Status | Meaning |
| --- | --- |
| `ok` | found |
| `MISSING` | a required part is absent; the command exits with 1 and `E_DOCTOR` |
| `unavailable` | an optional part (`stdlib`, `xcode`) is absent; the exit code stays 0 |

### `clean [-y] [--dry-run]`

Lists the cache entries with their sizes, then deletes them.

| Situation | Result |
| --- | --- |
| `--dry-run` | nothing is deleted, even with `-y` |
| `-y` / `--yes` | deleted without a question |
| a terminal, no `-y` | asks `Delete N entries (size)? [y/N]`; `y` or `yes` deletes |
| no terminal, no `-y` | nothing is deleted; the output says so |

Only the tool's own cache directories are removed. Other files in the cache
root stay.

### `mcp [--tools=<list>|all]`

Runs an MCP server on stdin and stdout. See [MCP tools](#mcp-tools).

## Exit codes and errors

| Exit code | Meaning |
| --- | --- |
| 0 | success, including a search without matches |
| 1 | any error |

An error is one line on stderr:

```text
E_CODE: message. Try: hint
```

Notes and progress lines on stderr start with `ksrc-native: `. Only results go
to stdout.

| Code | Meaning |
| --- | --- |
| `E_NO_KONAN` | no usable Kotlin/Native distribution |
| `E_NO_SOURCES` | unknown framework, target, stdlib module or SDK; a Swift-only framework; nothing to search |
| `E_NO_RG` | `rg` is not on `PATH` |
| `E_NO_XCODE` | `xcrun` or the developer tools are missing |
| `E_NO_FILE` | the file-id points at a file that does not exist |
| `E_BAD_COORD` | a coordinate cannot be parsed, has an unknown group, or versions disagree |
| `E_BAD_FILE_ID` | a file-id cannot be parsed |
| `E_BAD_LINES` | the `--lines` range cannot be parsed |
| `E_BAD_ARGS` | wrong arguments, an unknown flag or command |
| `E_KLIB_FAILED` | the `klib` tool failed, usually because no JDK is on `PATH` |
| `E_DUMP_FORMAT` | the `klib` output is not a metadata dump the tool understands |
| `E_RG_FAILED` | ripgrep failed, usually because of a bad pattern or argument |
| `E_CACHE` | the cache directory cannot be used |
| `E_DOCTOR` | `doctor` found a required part missing |
| `E_MCP` | the MCP session ended with a protocol error |
| `E_INTERNAL` | a failure without a code of its own |

## Environment

| Variable | Meaning |
| --- | --- |
| `KONAN_HOME` | path of the distribution; second in the discovery order |
| `KONAN_DATA_DIR` | directory that holds the distributions instead of `~/.konan` |
| `KSRC_NATIVE_CACHE_DIR` | cache directory instead of the user cache directory |

## Cache layout

```text
<cache root>/platform-dumps-v1/<key>/meta.json
<cache root>/platform-dumps-v1/<key>/src/<target>/<Class>.kt
<cache root>/platform-dumps-v1/<key>/src/<target>/_toplevel.kt
<cache root>/stdlib-v1/<key>/meta.json
<cache root>/stdlib-v1/<key>/src/...
```

`meta.json` marks an entry as complete. An entry is built in a temporary
sibling directory and renamed into place.

## MCP tools

`ksrc-native mcp` serves `search`, `cat` and `deps`. `--tools=where` or
`--tools=all` adds `where`. `tools/list` returns the tools sorted by name.

The global flags given to `ksrc-native mcp` are the defaults of every call.
An input of a call wins over them.

Inputs every tool accepts, all optional:

| Input | Type | Flag |
| --- | --- | --- |
| `konan_home` | string | `--konan-home` |
| `kotlin_version` | string | `--kotlin-version` |
| `project` | string | `--project` |
| `target` | string | `--target` |
| `sdk` | string | `--sdk` |

Inputs per tool:

| Tool | Input | Type | Required | Flag |
| --- | --- | --- | --- | --- |
| `search` | `pattern` | string | yes | the pattern argument |
| `search` | `modules` | array of strings | no | `--module`, repeated |
| `search` | `all_platform` | boolean | no | `--all-platform` |
| `search` | `context` | integer | no | `--context` |
| `search` | `rg_args` | array of strings | no | the arguments after `--` |
| `cat` | `file_id` | string | yes | the file-id argument |
| `cat` | `lines` | string | no | `--lines` |
| `cat` | `line_numbers` | boolean | no | `--line-numbers` |
| `deps` | | | | |
| `where` | `coordinate` | string | yes | the coordinate argument |

An input that is not in the table is rejected before the tool runs.

Results:

- Success: one text block with what the command prints to stdout. Notes from
  stderr, such as the progress line of a first build, follow as a second text
  block. When stdout is empty, the note is the result.
- Failure: `isError` is true and the text is the `E_CODE: message. Try: hint`
  line. A failed tool call is not a protocol error.
- A result longer than 256 KiB is cut at the end of a line, and a note says so.

There is no `clean` and no `doctor` tool.
