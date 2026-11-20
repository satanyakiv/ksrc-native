# Decisions

Dated entries, oldest first. Each entry says what was decided, why, and what
it costs. Add an entry when a choice would surprise a new contributor. Do not
rewrite an old entry: add a new one that supersedes it.

## 2026-11-19: Follow the conventions of ksrc, share no code

**Decision.** The tool uses the verbs, the output contract, the error format
and the repository layout of `respawn-llc/ksrc`. All code is written for this
project.

**Why.** An agent that knows `ksrc` can use `ksrc-native` without learning a
second interface: `search`, then `cat` with a file-id. The two tools cover
different sources and are used side by side.

**Cost.** A change of a convention in `ksrc` does not reach this tool by
itself. The project is independent and not affiliated with Respawn or
JetBrains.

## 2026-11-19: Synthetic coordinates for sources that are not Gradle modules

**Decision.** The three sources have coordinates in the Gradle shape
`group:name[:version]` with made-up groups: `kn.platform:<Framework>`,
`kn.stdlib:native`, `sdk.apple:<Framework>`.

**Why.** A file-id `group:name:version!/path` then works for every source, and
`cat` needs no flag to know where a file comes from. A group with a dot that no
real Maven group uses cannot be mistaken for a library.

**Cost.** The version part means two things. For `kn.*` it is a Kotlin
version. For `sdk.apple` it is an SDK name such as `iphoneos26.4`.

## 2026-11-19: One file per class, cut from the klib metadata dump

**Decision.** Platform klibs have no sources. The tool runs
`klib dump-metadata` and splits the output: one file per outermost class, and
one `_toplevel.kt` per framework for functions, properties and type aliases.
Nested classes and companion objects go into the file of their outer class.

**Why.** The dump of UIKit is one text of about 65,000 lines. A hit in it says
nothing about the class it belongs to. Small files make the file-id carry the
class name, make `cat` cheap, and let ripgrep limit a search with `-g`.

**Cost.** The files are signatures in the dump's own notation, with names like
`platform/UIKit/UIView`. They are not compilable Kotlin. The split depends on
the dump format; an unknown format fails with `E_DUMP_FORMAT` and writes
nothing to the cache.

## 2026-11-19: The Kotlin version comes from the distribution directory name

**Decision.** The version of a distribution is read from its directory name
(`kotlin-native-prebuilt-macos-aarch64-2.4.21`). The klib manifest is the
fallback.

**Why.** The manifest of the 2.4.21 stdlib says `2.4.20-dev-7885`. The
directory name is what Gradle uses and what the user expects to see.

## 2026-11-19: Cache keys follow the input, not the request

**Decision.** A platform dump is keyed by the real path of the klib and its
`compiler_version`. The extracted stdlib is keyed by the real path, size and
modification time of the zip. An entry is written to a temporary directory and
renamed into place; `meta.json` marks it complete.

**Why.** The path holds the Kotlin version, the target and the framework, so
each combination has its own entry. A distribution replaced in place gets a
new key. A build that is interrupted leaves no entry that looks complete, and
two processes that build the same entry do not corrupt each other.

**Cost.** Old entries stay until `ksrc-native clean`. The cache directories
carry a format version (`platform-dumps-v1`); a change of the file layout must
raise it.

## 2026-11-19: Apple headers are searched in place

**Decision.** `sdk.apple` modules are not cached. ripgrep runs on the
`Headers` directory of the framework inside the selected Xcode.

**Why.** Headers are plain text already. A copy would cost disk space and
could go stale after an Xcode update.

**Cost.** The file-id holds the SDK name, not a path. A file-id from one
machine works on another only when that SDK is installed there.

## 2026-11-19: A search never builds all platform dumps unasked

**Decision.** `search` without `--module` covers the dumps that exist already,
plus the stdlib. Only `--all-platform` builds every library of the target.

**Why.** `ios_arm64` has 177 platform libraries. Building all of them starts
the JVM 177 times and takes minutes. A command that is usually instant must
not do that because a flag was left out.

**Cost.** A search without `--module` can miss a framework that was never
searched before. The note on stderr names what was searched.

## 2026-11-19: Xcode and the stdlib sources are optional

**Decision.** `doctor` reports a missing Xcode or stdlib zip as `unavailable`
and exits with 0. `deps` skips a source that is not available. `sdk.apple`
works without Kotlin/Native, and `kn.*` works without Xcode.

**Why.** Each source is useful alone. A Linux machine has no Xcode; a machine
with Xcode may not have run a Kotlin/Native build yet.

**Cost.** `deps` exits with 0 while a source is missing. It fails only when
nothing can be listed, or when `--konan-home` or `--kotlin-version` names a
distribution that cannot be used: a typo must not hide behind a list of SDKs.

## 2026-11-19: Every error exits with 1 and prints one coded line

**Decision.** An error is `E_CODE: message. Try: hint` on stderr with exit
code 1. A search without matches is not an error. A test builds the binary and
checks the exit code of each kind of failure.

**Why.** Agents and scripts act on the exit code and the code prefix. A hint
in the same line lets an agent correct the call without another lookup.

## 2026-11-19: MCP results are plain text, failures are tool results

**Decision.** An MCP tool returns the text its command prints. There is no
output schema and no structured content. A failure is a result with `isError`
and the `E_CODE` line, not a protocol error. A result is cut at 256 KiB.
The inputs are the command's flags in snake_case.

**Why.** The command line and the MCP server share one adapter layer, so both
give the same answer for the same request, and one skill describes both. A
model can read a failed result and retry; it cannot see a protocol error.
The cap keeps one broad search from filling the model's context.

**Cost.** A client that wants fields must parse the
`<file-id> <line>:<col>:<text>` line.

## 2026-11-19: The plugins carry the skill, not the binary and not the MCP server

**Decision.** The Claude Code and Codex plugins install
`skills/ksrc-native/SKILL.md` only. The binary is installed separately. The
MCP server is a separate, optional way to use the tool. The repository is its
own marketplace, named `ksrc-native`.

**Why.** A plugin cannot ship one binary for every system. A skill that runs
the command costs about 190 tokens per session; an MCP server adds tool
schemas to every session and gives the same answers.

**Cost.** Two install steps. The version is written in `VERSION` and in the
manifests; a test keeps them equal.

## 2026-11-19: No Windows in 0.1; releases come from scripts

**Decision.** Releases are built for macOS and Linux, arm64 and amd64.
`scripts/build.sh` and `scripts/package.sh` build them; there is no Makefile
and no release tool beyond the workflow.

**Why.** The Apple sources exist on macOS only. Linux is useful for
`kn.platform` with Linux targets and for CI. Nobody has asked for Windows, and
the tests do not run there.

**Cost.** Windows users must build from source and test it themselves.

## 2026-11-20: Values from `--` and from MCP inputs are untrusted

**Decision.** Every value that arrives after `--` on the command line or
through an input of an MCP tool is checked before it reaches a program or a
path. The tools are announced as read-only, so no input may run a program or
read a file outside the three sources.

- The rg arguments pass an allow-list, `search.NormalizeExtraArgs`: ten flags
  without a value and six with one. They are rewritten to `--flag` and
  `--flag=value`, so a value is never read as a flag. The pattern travels as
  one `--regexp=<pattern>` argument. rg runs with `--no-config`. A hit outside
  the searched directories is an error, not a line of output.

**Why.** A model fills these fields, and a model reads files it did not
write. Before this change `-- --pre <script>` ran the script, and a path after
`--` printed lines of any file the user can read. A README in a hostile
repository could ask for either.

**Cost.** An rg flag outside the list fails with `E_BAD_ARGS`, also for a
person at a terminal: no `-l`, no `-c`, no `--hidden`, no `-t`. `-m1` must be
written `-m 1`. A new flag needs a line in the list and a test.
