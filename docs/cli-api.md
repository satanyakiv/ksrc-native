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

The name of a `kn.platform` module is the name of a platform library of the
distribution. It is usually the package after `platform.`, but not always: in
Kotlin 2.4.21 the class `platform.CoreLocation.CLLocation` is in the library
`_LocationEssentials`. `ksrc-native deps` lists every library.

All `kn.*` modules of one command must name the same Kotlin version. The
version part of an `sdk.apple` coordinate is an SDK name, so it may differ.

## Global flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--konan-home <path>` | discovered | path of the Kotlin/Native distribution |
| `--kotlin-version <version>` | discovered | Kotlin version of the distribution to use, for example `2.4.21` |
| `--project <dir>` | `.` | project directory; the Kotlin version is read from its `gradle/libs.versions.toml` |
| `--target <target>` | `ios_arm64`, or the host target | Kotlin/Native target of the platform libraries |
| `--sdk <sdk>` | `iphoneos` | Apple SDK of `sdk.apple` modules |
| `--version`, `-v` | | print `ksrc-native <version>` |
| `--help`, `-h` | | help for the tool or a command |

The distribution is picked in this order: `--konan-home`; the `KONAN_HOME`
variable; `--kotlin-version` or the version in a `kn.*` coordinate;
`kotlin = "..."` in `gradle/libs.versions.toml` of `--project`; the newest
distribution under `KONAN_DATA_DIR` or `~/.konan`. When the project's version
is not installed, the newest one is used and a note on stderr says so:
`ksrc-native: the project uses Kotlin <version>, but ...; using <version>`.

An SDK named in an `sdk.apple` coordinate wins over `--sdk`.

Without `--target` the target is `ios_arm64` when the distribution has its
platform libraries. A distribution for Linux or Windows has no Apple targets.
There the tool uses the host target: first the one in the name of the
distribution directory (`kotlin-native-prebuilt-linux-x86_64-<version>` gives
`linux_x64`), then the one of the system the tool runs on (`linux_x64`,
`linux_arm64`, `macos_arm64`, `macos_x64`, `mingw_x64`). When the
distribution has neither, the target stays `ios_arm64` and a `kn.platform`
command fails with `E_NO_SOURCES` and the list of targets. A target given
with `--target` is never replaced.

`--target` and `--sdk` are checked before anything is read or built:

- A target must be the exact name of a target of the distribution, as
  `ksrc-native doctor` lists them: `ios_arm64`, not `IOS_ARM64` and not a path
  such as `../platform/ios_arm64`. Anything else fails with `E_NO_SOURCES`
  and a hint that lists the targets. The target segment of a `kn.platform`
  file-id is checked the same way.
- An SDK must be a name: a letter followed by letters, digits and dots, for
  example `iphoneos`, `macosx` or `iphoneos26.4`. A path fails with
  `E_BAD_ARGS` (`E_BAD_COORD` when it is the SDK part of a coordinate), and
  `xcrun` is not run.

## Commands

### `search <pattern> [--module <coordinate>]... [-- <rg-args>]`

Alias: `rg`. Runs ripgrep over the modules.

| Flag | Meaning |
| --- | --- |
| `--module`, `-m` | module to search; repeatable |
| `--all-platform` | build and search every platform library of the target |
| `--context`, `-C` | lines of context around each match |
| `-- <rg-args>` | ripgrep arguments from the allow-list below, for example `-i`, `-F`, `-g '*Delegate*'` |

Arguments after `--` are checked before anything runs. These pass:

| Argument | Meaning in ripgrep |
| --- | --- |
| `-i`, `--ignore-case` | ignore case |
| `-s`, `--case-sensitive` | match case |
| `-S`, `--smart-case` | ignore case unless the pattern has an upper-case letter |
| `-F`, `--fixed-strings` | the pattern is a literal string |
| `-w`, `--word-regexp` | match whole words |
| `-x`, `--line-regexp` | match whole lines |
| `-v`, `--invert-match` | print the lines that do not match |
| `-U`, `--multiline` | let a match span lines |
| `--multiline-dotall` | with `-U`, let `.` match a line break |
| `-P`, `--pcre2` | use the PCRE2 engine |
| `-g <glob>`, `--glob <glob>` | only files whose name matches the glob; `!` negates |
| `--iglob <glob>` | the same, ignoring case |
| `-m <n>`, `--max-count <n>` | at most `n` matching lines per file |
| `-A <n>`, `--after-context <n>` | `n` lines after each match |
| `-B <n>`, `--before-context <n>` | `n` lines before each match |
| `--max-columns <n>` | leave out lines longer than `n` bytes |

A value is written as `--flag value`, `--flag=value` or `-g value`. Short flags
without a value can be joined: `-iF`. A number is a non-negative integer.

Everything else fails with `E_BAD_ARGS` and a hint that lists the table above:
an unknown flag, a flag that changes the output (`-l`, `-c`, `--files`,
`--no-json`, `--vimgrep`), `--pre`, `--pre-glob`, `-f`, `-L`, `--hidden`, `-u`,
`--no-ignore`, `-z`, a path, a second `--`, a flag without its value. Use
`--context` of ksrc-native in place of rg's `-C`.

The pattern is a ripgrep regular expression. Two rules apply to it:

- A pattern that is empty or only white space fails with `E_BAD_ARGS`.
- On the command line a pattern cannot start with `-`: the argument would be
  read as a flag, and `ksrc-native search -foo` fails with `E_BAD_ARGS`. It
  cannot go after `--` either, because that place belongs to the rg
  arguments. Write `'[-]foo'` or `'\-foo'`. The `pattern` input of the MCP
  tool has no such limit.

The pattern is passed to ripgrep as a pattern whatever it looks like. The
user's ripgrep configuration file is not read.

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
| `--lines A,B` | line range, 1-based and inclusive; default: the whole file |
| `--line-numbers`, `-n` | prefix every line with its number and a tab |

Forms of a line range:

| Value | Lines printed |
| --- | --- |
| `A,B` | from `A` to `B` |
| `A,` | from `A` to the end of the file |
| `A` | the line `A` alone |
| `A-B`, `A:B` | the same as `A,B`; `A-` and `A:` are the same as `A,` |

`A` is 1 or more and `B` is not below `A`. An end past the last line is cut
to the last line, so `--lines 700,99999` prints to the end. A start past the
last line fails with `E_BAD_LINES`, and the message gives the number of lines
of the file. So does a value that is not a range, such as `0,5`, `5,2` or
`1,2,3`.

A file-id is `group:name:version!/inner/path`:

```text
kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt
kn.stdlib:native:2.4.21!/nativeMain/kotlinx/cinterop/Utils.kt
sdk.apple:UIKit:iphoneos26.4!/UIViewController.h
```

For `kn.platform` the first path segment is the target, so `--target` is not
needed. The segment must be the exact name of a target, like `--target`.
`cat` builds a missing dump the same way `search` does.

### `deps`

Lists what can be searched, one module per line:

```text
kn.platform:<Framework>:<kotlinVersion> (<target>, dump cached: yes|no)
kn.stdlib:native:<kotlinVersion> (extracted: yes|no, <zip path>)
sdk.apple:<Framework>:<sdk> (<n> frameworks with headers, <sdk path>)
```

The `sdk.apple` line stands for all frameworks of one installed SDK;
`<Framework>` is literal there. The `kn.stdlib` line needs no target: it is
printed for a distribution that lacks the platform libraries of the target
too. Off macOS the `sdk.apple` lines are left out without a note. A source
that is not available is reported on
stderr as `ksrc-native: skipped: <error>` and left out. The command fails when
nothing can be listed, when `--konan-home`, the `KONAN_HOME` variable or
`--kotlin-version` names a distribution that cannot be used, or when
`--target` names a target the distribution that was found does not have. Only a
distribution that nobody named is skipped: no flag, no variable, and nothing
under `~/.konan`.

### `where <coordinate>`

Prints one path: the klib directory of a platform library, the stdlib sources
zip, or the `Headers` directory of an SDK framework. It builds nothing.

### `doctor`

Prints one line per part: `rg`, `kotlin-native`, `klib`, `java`, `platform`,
`stdlib`, `xcode`, `cache`. Each line is `<part>: <status> - <details>`.

| Status | Meaning |
| --- | --- |
| `ok` | found and usable |
| `MISSING` | a required part is absent or does not work; the command exits with 1 and `E_DOCTOR` |
| `unavailable` | an optional part (`stdlib`, `xcode`) is absent; the exit code stays 0 |

The `platform` line names the target. When no `--target` was given and the
distribution has no `ios_arm64`, it reads `<n> libraries for linux_x64, the
host target: the distribution has no ios_arm64 (...)`. Off macOS the `xcode`
line is `unavailable` and no program is run for it.

Two lines do more than look for a file:

- `java` finds the program the way the `klib` tool does
  (`$JAVA_HOME/bin/java` when it is executable, else `java` on `PATH`) and
  runs `java -version`. On macOS `/usr/bin/java` exists without a JDK and
  fails when it is run. A java that is absent or fails is `MISSING`:
  `kn.platform` cannot build a dump without it. Without a Kotlin/Native
  distribution nothing needs java, and the line is `unavailable`.
- `cache` checks that the cache directory, or the nearest directory above it
  when it does not exist yet, can be written. A file in the way and a
  read-only directory are `MISSING`, with the same `E_CACHE` text a search
  would fail with. The check creates nothing.

### `clean [-y] [--dry-run]`

Lists the cache entries with their sizes, then deletes them.

| Situation | Result |
| --- | --- |
| `--dry-run` | nothing is deleted, even with `-y` |
| `-y` / `--yes` | deleted without a question |
| a terminal, no `-y` | asks `Delete N entries (size)? [y/N]`; `y` or `yes` deletes |
| no terminal, no `-y` | nothing is deleted; the output says so |

A count of one is written in the singular: `total: 1 entry`, `Delete 1 entry
(size)?`, `deleted 1 entry`, and `1 platform dump` in the `cache` line of
`doctor`.

Only the tool's own cache directories are removed. Other files in the cache
root stay.

### `mcp [--tools=<list>|all] [--max-result-bytes=<n>]`

Runs an MCP server on stdin and stdout. See [MCP tools](#mcp-tools).

| Flag | Meaning |
| --- | --- |
| `--tools` | the tools to serve, and no others: a comma-separated list of `search`, `cat`, `deps`, `where`, or `all`; default `search,cat,deps` |
| `--max-result-bytes` | largest text of one tool result in bytes; default 65536, minimum 4096; a smaller value or one that is not a number fails with `E_BAD_ARGS` |

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
| `E_NO_XCODE` | `xcrun` or the developer tools are missing; on Linux every `sdk.apple` module ends here |
| `E_NO_FILE` | the file-id points at a file that does not exist |
| `E_BAD_COORD` | a coordinate cannot be parsed, has an unknown group, or versions disagree |
| `E_BAD_FILE_ID` | a file-id cannot be parsed |
| `E_BAD_LINES` | the `--lines` range cannot be parsed, or it starts past the end of the file |
| `E_BAD_ARGS` | wrong arguments, an unknown flag or command, an empty pattern, an rg argument that is not allowed, an `--sdk` value that is not an SDK name, an unknown MCP tool, a `--max-result-bytes` below 4096 |
| `E_KLIB_FAILED` | the `klib` tool failed, usually because no JDK runs; the message has the exit status and the first 300 bytes of what `klib` printed, on one line; the `java` line of `doctor` shows the state of the JVM |
| `E_DUMP_FORMAT` | the `klib` output is not a metadata dump the tool understands: it has no `library fragment` block, or a block does not end where expected; nothing is cached |
| `E_RG_FAILED` | ripgrep failed, usually because of a bad pattern or argument |
| `E_CACHE` | the cache directory cannot be used |
| `E_TIMEOUT` | a program the tool started ran past its deadline and was stopped |
| `E_CANCELLED` | the run was interrupted (Ctrl-C, `SIGTERM`, a cancelled MCP call) while a program was running |
| `E_DOCTOR` | `doctor` found a required part missing |
| `E_MCP` | the MCP session ended with a protocol error |
| `E_INTERNAL` | a failure without a code of its own |

### Deadlines and interrupts

The tool starts five programs. Each has a deadline:

| Program | Deadline |
| --- | --- |
| `klib` (one dump) | 120 s |
| `rg` | 60 s |
| `xcrun`, `xcode-select` | 15 s |
| `java -version` (only in `doctor`) | 15 s |

A program that runs longer is stopped, and the command fails with
`E_TIMEOUT`. Ctrl-C, `SIGTERM` and a cancelled MCP call stop the running
program at once, and the command fails with `E_CANCELLED`. In both cases the
tool kills the program together with every process it started (`klib` is a
shell script that starts a JVM), and a half-built dump is not kept in the
cache.

## Environment

| Variable | Meaning |
| --- | --- |
| `KONAN_HOME` | path of the distribution; second in the discovery order. A value that is set and is not a distribution fails every `kn.*` operation and `deps` with `E_NO_KONAN` |
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

`<Class>` is the name of the outermost class. A name longer than 200 bytes is
cut to 200 and gets a dash and 8 hex digits of a hash; the file content keeps
the full name, so a search for it still hits.

`meta.json` marks an entry as complete. An entry is built in a temporary
sibling directory, `.tmp-<random>`, and renamed into place. A name that
starts with a dot is never read as an entry. A build removes the temporary
directories that are older than one hour: they are what a killed build left
behind.

The key of a platform dump is a hash of the real path of the klib, the
`compiler_version` in its manifest and the format version of the splitter.
`meta.json` records that version as `format`. After an upgrade of the tool
that changes the split files, a library is dumped again on its next use. The
entry of the other format is never read again. It is removed when the same
library is used: a note on stderr says `ksrc-native: removed 1 cache entry of
another dump format for <coordinate> (<target>)`. A library that is not used
again keeps its old entry until `ksrc-native clean`; `doctor` and `clean`
count such an entry.

## MCP tools

`ksrc-native mcp` serves `search`, `cat` and `deps`. `--tools` replaces that
list; it does not add to it. `--tools=where` serves `where` alone. To serve
`where` next to the default tools, name all four,
`--tools=search,cat,deps,where`, or write `--tools=all`. A name that is not a
tool fails with `E_BAD_ARGS`. `tools/list` returns the tools sorted by name.

The global flags given to `ksrc-native mcp` are the defaults of every call.
An input of a call wins over them.

`--konan-home` is the exception: no tool has a `konan_home` input. The server
runs the `klib` program of the distribution, so only the person who starts the
server chooses it, with `ksrc-native --konan-home <path> mcp` or the
`KONAN_HOME` variable. A call that sends `konan_home` is rejected.

Inputs every tool accepts, all optional:

| Input | Type | Flag |
| --- | --- | --- |
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
| `search` | `rg_args` | array of strings | no | the arguments after `--`, one item per word, same allow-list |
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
- The text of one result, all blocks together, is at most 64 KiB (65536
  bytes), or the value of `--max-result-bytes`. Notes get an eighth of that.
  Longer output is cut at the end of a line, and the last line of the text
  block says so: `ksrc-native: output truncated at <kept> bytes, the limit of
  one result is <limit>; ...`. The note is counted in the limit. A search
  stops ripgrep at that point, so a broad pattern costs no more time than a
  narrow one.

There is no `clean` and no `doctor` tool.
