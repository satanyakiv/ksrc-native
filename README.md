# ksrc-native

`ksrc-native` is a command-line tool that searches and reads the sources behind
Kotlin/Native code on Apple platforms. It answers three questions an agent or a
developer has while writing `iosMain` code:

- What does this Apple API look like from Kotlin? (`platform.UIKit`, `platform.Foundation`, ...)
- What does this `kotlinx.cinterop` or native stdlib function do?
- What does the Objective-C header say about it?

## The problem

Code in `iosMain` calls Apple frameworks through `platform.UIKit`,
`platform.Foundation` and the other `platform.*` packages. The names in those
packages are not the names in Apple's documentation. The `cinterop` tool
generates them from the Objective-C headers, and the result has its own shape:

| Where you look | What you see |
| --- | --- |
| Swift documentation | `present(_:animated:completion:)` |
| Objective-C header | `- (void)presentViewController:(UIViewController *)viewControllerToPresent animated:(BOOL)flag completion:(void (^)(void))completion` |
| Kotlin, which is what `iosMain` must call | `fun presentViewController(viewControllerToPresent: UIViewController, animated: Boolean, completion: (() -> Unit)?)` |

Protocols get a suffix (`WKNavigationDelegateProtocol`), constants get nullable
types (`NSURLErrorDomain: String?`), and so on. To write the call, you need the
Kotlin line.

That line is hard to get outside an IDE:

- Kotlin/Native ships these bindings as `.klib` files with metadata and no
  source files. There is nothing to grep.
- [`ksrc`](https://github.com/respawn-llc/ksrc) searches the sources of Gradle
  dependencies and of the Android SDK. Platform klibs are not a Gradle
  dependency, so a search there ends with `E_NO_SOURCES`.
- A coding agent works in a terminal. Without a lookup it writes the call from
  memory, and a wrong guess shows up only after a slow Kotlin/Native compile.

The same gap exists for two neighbours of the platform klibs: the native stdlib
with `kotlinx.cinterop` (`memScoped`, `CPointer`, `alloc`), whose sources sit in
a zip inside the Kotlin/Native distribution, and the Xcode SDK headers, which
hold the documentation comments.

`ksrc-native` closes this gap. One command returns the Kotlin signature that
the project's own Kotlin/Native version generates:

```console
$ ksrc-native search 'fun presentViewController\(' --module kn.platform:UIKit
kn.platform:UIKit:2.4.21!/ios_arm64/UIViewController.kt 104:24:  public open external fun presentViewController(viewControllerToPresent: platform/UIKit/UIViewController, animated: kotlin/Boolean, completion: kotlin/Function0<kotlin/Unit>?): kotlin/Unit
```

## Why not another solution

We looked at the alternatives before we wrote a new tool.

| Alternative | Why it was not enough |
| --- | --- |
| A change in `ksrc` itself | `ksrc` names source search outside Gradle dependencies and Android SDK platform sources as a non-goal in its `docs/decisions.md`. The three sources here also need no Gradle resolution at all: they come from the Kotlin/Native distribution and from Xcode. A sibling tool with the same verbs keeps `ksrc` small and lets an agent use both with one habit. |
| An existing MCP server for klib metadata ([`kmp-api-lookup-mcp`](https://www.npmjs.com/package/kmp-api-lookup-mcp)) | It reads the same data, so the idea is sound. Version 0.1.1 did not start for us in November 2026, because one of its dependencies resolved to a newer release with a different export. We also wanted one static binary with no Node.js runtime, and a plain CLI that works in a terminal and in a skill, not only through MCP. |
| A shell script around `klib dump-metadata` and `grep` | It works in one repository. It has no cache that survives a Kotlin upgrade, no stable output contract, no MCP server, no tests, and nothing to install as a plugin. This was our first plan, and it is how the idea was checked. |
| Apple's documentation, or the Xcode headers alone | They show the Swift and Objective-C shape, not the Kotlin one. The headers are still worth reading for their comments, so they are the third source of this tool, not a replacement for the first. |
| "Go to declaration" in an IDE | It shows the same generated stubs, but only to a person who has the project open in the IDE. An agent in a terminal cannot use it, and it cannot search a whole framework for a pattern. |
| The model's memory, or a web search | An answer from memory is not tied to the Kotlin/Native version that the project builds with, and nothing shows when it is wrong until the compiler does. |

This project is independent. It is not affiliated with Respawn or JetBrains.
It follows the conventions of [`respawn-llc/ksrc`](https://github.com/respawn-llc/ksrc)
on purpose: the same verbs, the same output contract, the same error format.
`ksrc` searches the sources of Gradle dependencies. `ksrc-native` covers what
`ksrc` does not: Kotlin/Native platform klibs, the native stdlib and Xcode SDK
headers. The two tools work side by side. All code here is written for this
project; no file is copied from `ksrc`.

## What it is

`ksrc-native` runs the `klib` tool of the Kotlin/Native distribution once per
framework, splits its metadata dump into one file per class and caches the
result. After that, a lookup is a ripgrep search over small text files.

It reads three kinds of modules:

| Module | Content | Needs |
| --- | --- | --- |
| `kn.platform:<Framework>[:<kotlinVersion>]` | Kotlin signatures of `platform.<Framework>` | a Kotlin/Native distribution, a JDK |
| `kn.stdlib:native[:<kotlinVersion>]` | sources of the native stdlib and `kotlinx.cinterop` | a Kotlin/Native distribution |
| `sdk.apple:<Framework>[:<sdk>]` | Objective-C headers of an Xcode SDK | Xcode |

Requirements:

- [`rg`](https://github.com/BurntSushi/ripgrep) (ripgrep) on `PATH`.
- A Kotlin/Native distribution under `~/.konan`. Gradle downloads it the first
  time a Kotlin/Native task runs, for example `./gradlew compileKotlinIosArm64`.
- A JDK on `PATH` for `kn.platform` modules. The `klib` tool runs on the JVM.
- Xcode for `sdk.apple` modules. The other two kinds work without it.

macOS is the main platform. Linux binaries are built as well. On Linux
`kn.platform` and `kn.stdlib` work, and there are no `sdk.apple` modules:
Apple SDKs come with Xcode. A Kotlin/Native distribution for Linux has no
Apple targets, so without `--target` the tool uses the host target of the
distribution, `linux_x64`, and `doctor` says so in its `platform` line. Kotlin
publishes a Linux distribution for x86_64 only (checked on Maven Central for
2.2.20 and 2.3.0). The linux/arm64 binary needs a distribution you provide
with `--konan-home` or `KONAN_HOME`. Windows is not supported in 0.1.

`ksrc-native doctor` shows what the tool found:

```console
$ ksrc-native doctor
rg: ok - /opt/homebrew/bin/rg (ripgrep 15.2.0)
kotlin-native: ok - 2.4.21 at ~/.konan/kotlin-native-prebuilt-macos-aarch64-2.4.21 (also installed: 2.4.20, 2.4.10)
klib: ok - ~/.konan/kotlin-native-prebuilt-macos-aarch64-2.4.21/bin/klib
java: ok - /usr/bin/java (openjdk version "17.0.14" 2025-01-21)
platform: ok - 177 libraries for ios_arm64 (21 targets in the distribution)
stdlib: ok - ~/.konan/kotlin-native-prebuilt-macos-aarch64-2.4.21/sources/kotlin-stdlib-native-sources.zip (extracted: yes)
xcode: ok - SDK iphoneos26.4 at /Applications/Xcode-26.4.0.app/Contents/Developer/Platforms/iPhoneOS.platform/Developer/SDKs/iPhoneOS26.4.sdk (195 frameworks with headers)
cache: ok - ~/Library/Caches/ksrc-native (7 platform dumps, 14.3 MB)
```

## 1. Install the tool

With Go 1.27 or newer:

```bash
go install github.com/satanyakiv/ksrc-native/cmd/ksrc-native@latest
```

Without Go, install a standalone binary from GitHub Releases. The script picks
the archive for your system (macOS or Linux, arm64 or amd64), checks its
sha256 and puts the binary into `~/.local/bin`:

```bash
curl -fsSL https://raw.githubusercontent.com/satanyakiv/ksrc-native/main/scripts/install.sh | sh
```

The script reads two variables:

- `KSRC_NATIVE_VERSION`: the version to install, for example `0.1.0`. Default: the latest release.
- `KSRC_NATIVE_PREFIX`: the install prefix; the binary goes to `<prefix>/bin`. Default: `~/.local`.

Check the result:

```console
$ ksrc-native --version
ksrc-native 0.1.0
```

## 2. Teach agents how to use it

The binary must be on `PATH` first. Then pick one of the four ways below. The
plugins install a skill that tells the agent when and how to run the command.

### Claude Code plugin

In Claude Code:

```text
/plugin marketplace add satanyakiv/ksrc-native
/plugin install ksrc-native@ksrc-native
```

The same from a shell:

```bash
claude plugin marketplace add satanyakiv/ksrc-native
claude plugin install ksrc-native@ksrc-native
```

The plugin adds one skill, `ksrc-native:ksrc-native`. It costs about 190
tokens per session until the agent uses it.

### Codex plugin

```bash
codex plugin marketplace add satanyakiv/ksrc-native
codex plugin add ksrc-native@ksrc-native
```

### MCP

For an agent that talks MCP instead of running a shell, `ksrc-native mcp`
serves the same operations as tools over stdio:

```console
$ claude mcp add ksrc-native -- ksrc-native mcp
Added stdio MCP server ksrc-native with command: ksrc-native mcp to local config
$ claude mcp list
ksrc-native: ksrc-native mcp - ✔ Connected
```

For Codex: `codex mcp add ksrc-native -- ksrc-native mcp`.

The server offers `search`, `cat` and `deps`. `--tools` replaces that list:
`ksrc-native mcp --tools=search,cat,deps,where` or `--tools=all` serves
`where` as well, and `--tools=where` serves `where` alone. A tool takes the flags of its command as inputs in snake_case
(`pattern`, `modules`, `file_id`, `lines`, `kotlin_version`, `target`, `sdk`)
and returns what the command prints. [docs/cli-api.md](docs/cli-api.md) lists
every input.

One result is at most 64 KiB of text. Longer output is cut at the end of a
line, the last line says so, and the search stops there.
`ksrc-native mcp --max-result-bytes <n>` sets another limit.

The inputs are checked as untrusted values. A tool call cannot choose the
Kotlin/Native distribution: `--konan-home` is a flag of the server
(`ksrc-native --konan-home <path> mcp`), not an input. `rg_args` takes the
same fixed list of ripgrep arguments as the command line.

### AGENTS.md prompt

For any other agent, paste this into the project's `AGENTS.md`:

```markdown
## Kotlin/Native API lookup

Use `ksrc-native` before you write or fix code that imports `platform.*` or
`kotlinx.cinterop`. Do not guess the shape of an iOS API and do not read
`~/.konan` or Xcode directories directly.

- Find a declaration: `ksrc-native search 'class platform/UIKit/UIApplication ' --module kn.platform:UIKit`
- Read the file of a hit: `ksrc-native cat '<file-id>' --lines 1,60`
- Native stdlib and cinterop sources: `--module kn.stdlib:native`
- Objective-C headers with Apple's comments: `--module sdk.apple:UIKit`
- Names in a platform dump are fully qualified with slashes: `platform/UIKit/UIView`, `kotlin/String`.
- An error prints `E_CODE: message. Try: hint`. Follow the hint.
```

## Usage

### Search, then cat

Every lookup has two steps. `search` prints one line per match:
`<file-id> <line>:<col>:<text>`. `cat` prints the file that a file-id names.

```console
$ ksrc-native search 'class platform/UIKit/UIApplication ' --module kn.platform:UIKit
kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt 2:13:public open class platform/UIKit/UIApplication : platform/UIKit/UIResponder {

$ ksrc-native cat 'kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt' --lines 1,6
@kotlinx/cinterop/ExternalObjCClass
public open class platform/UIKit/UIApplication : platform/UIKit/UIResponder {

  @kotlinx/cinterop/ObjCConstructor(designated = true, initSelector = "init")
  public /* secondary */ constructor()

```

Quote a file-id: it contains `!`. A pattern is a ripgrep regular expression.
It cannot be empty, and on the command line it cannot start with `-`: write
`'[-]foo'` in place of `'-foo'`.

`cat --lines A,B` prints the lines from `A` to `B`, counted from 1. `A,` runs
to the end of the file and `A` alone is one line. `A-B` and `A:B` mean the
same as `A,B`. An end past the last line is cut to the last line.

Arguments after `--` go to ripgrep. Only a fixed list passes: `-i`, `-F`,
`-w`, `-g <glob>`, `--max-count <n>` and a few more. A path or any other flag
fails with `E_BAD_ARGS`. The full list is in [docs/cli-api.md](docs/cli-api.md).

### Platform APIs: `kn.platform`

The framework is the name after `import platform.`. Names in a dump are fully
qualified with slashes: `platform/UIKit/UIView`, `kotlin/String`.

Find a method by its Kotlin name, inside one class:

```console
$ ksrc-native search 'fun openURL\(' --module kn.platform:UIKit -- -g UIApplication.kt
kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt 14:24:  public open external fun openURL(url: platform/Foundation/NSURL): kotlin/Boolean
kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt 20:24:  public open external fun openURL(url: platform/Foundation/NSURL, options: kotlin/collections/Map<kotlin/Any?, *>, completionHandler: kotlin/Function1<kotlin/Boolean, kotlin/Unit>?): kotlin/Unit
```

Find a constant. Top-level functions, constants and type aliases of a
framework are in `_toplevel.kt`:

```console
$ ksrc-native search 'val NSURLErrorDomain:' --module kn.platform:Foundation
kn.platform:Foundation:2.4.21!/ios_arm64/_toplevel.kt 7368:14:public final val NSURLErrorDomain: kotlin/String? /* = platform/Foundation/NSErrorDomain^? */
```

More patterns:

| You look for | Pattern | Where it lands |
| --- | --- | --- |
| a class | `'class platform/UIKit/UIApplication '` | `UIApplication.kt` |
| a protocol (Kotlin adds `Protocol`) | `'interface platform/WebKit/WKNavigationDelegateProtocol '` | `WKNavigationDelegateProtocol.kt` |
| a method by Objective-C selector | `'selector = "openURL:options:completionHandler:"'` | the line above the `fun` |
| class (static) methods | `'fun sharedApplication'` | `UIApplicationMeta.kt` |

A few classes live in a library with another name than their package. In
Kotlin 2.4.21 `platform.CoreLocation.CLLocation` is in
`kn.platform:_LocationEssentials`, not in `kn.platform:CoreLocation`. When a
class is not in the framework its package names, look at the libraries whose
name starts with `_` in `ksrc-native deps`, or search with `--all-platform`.

The first search of a framework builds its dump. On an Apple silicon Mac that
took 0.3 s for CoreLocation and about 5 s for UIKit. Later searches read the
cache and take about 0.01 s.

`--target` picks the Kotlin/Native target. The default is `ios_arm64`, or the
host target (for example `linux_x64`) in a distribution without it. Use
`ios_simulator_arm64`, `macos_arm64` and so on when an API differs per target.
The value must be the exact name of a target of the distribution;
`ksrc-native doctor` shows how many there are, and the error for a wrong one
lists them.

### Native stdlib: `kn.stdlib`

```console
$ ksrc-native search 'fun <R> memScoped' --module kn.stdlib:native
kn.stdlib:native:2.4.21!/nativeMain/kotlinx/cinterop/Utils.kt 715:15:public inline fun <R> memScoped(block: MemScope.()->R): R {

$ ksrc-native cat 'kn.stdlib:native:2.4.21!/nativeMain/kotlinx/cinterop/Utils.kt' --lines 709,715 -n
709	/**
710	 * Runs given [block] providing allocation of memory
711	 * which will be automatically disposed at the end of this scope.
712	 */
713	@ExperimentalForeignApi
714	@OptIn(kotlin.contracts.ExperimentalContracts::class)
715	public inline fun <R> memScoped(block: MemScope.()->R): R {
```

These are ordinary Kotlin sources. Native-only code is under `nativeMain/`.

### Xcode SDK headers: `sdk.apple`

A header has what a Kotlin signature lacks: Apple's comment, the availability
macro and the nullability annotation.

```console
$ ksrc-native search 'presentViewController:' --module sdk.apple:UIKit -- -F --max-count 1
sdk.apple:UIKit:iphoneos26.4!/UIViewController.h 262:9:- (void)presentViewController:(UIViewController *)viewControllerToPresent animated: (BOOL)flag completion:(void (^ __nullable)(void))completion NS_SWIFT_DISABLE_ASYNC API_AVAILABLE(ios(5.0));
```

`--sdk` picks the SDK. The default is `iphoneos`. Other values are
`iphonesimulator`, `macosx`, or a full name from `ksrc-native deps` such as
`iphoneos26.4`. It takes a name, not a path. Headers are searched in place;
nothing is cached. A Swift-only framework (SwiftData, Charts) has no headers
and no `platform.*` library.

### Without `--module`

A search without `--module` covers the platform dumps that are cached already,
plus the stdlib. It never builds all frameworks on its own. `--all-platform`
does that: it builds and searches every platform library of the target, which
takes minutes the first time.

### What can be searched, and where it is

`deps` lists the modules. `where` prints the path behind one module and builds
nothing.

```console
$ ksrc-native deps
kn.platform:ARKit:2.4.21 (ios_arm64, dump cached: no)
kn.platform:AVFAudio:2.4.21 (ios_arm64, dump cached: no)
...
kn.stdlib:native:2.4.21 (extracted: yes, ~/.konan/kotlin-native-prebuilt-macos-aarch64-2.4.21/sources/kotlin-stdlib-native-sources.zip)
...
sdk.apple:<Framework>:iphoneos26.4 (195 frameworks with headers, /Applications/Xcode-26.4.0.app/Contents/Developer/Platforms/iPhoneOS.platform/Developer/SDKs/iPhoneOS.sdk)
...

$ ksrc-native where sdk.apple:UIKit --sdk iphonesimulator
/Applications/Xcode-26.4.0.app/Contents/Developer/Platforms/iPhoneSimulator.platform/Developer/SDKs/iPhoneSimulator26.4.sdk/System/Library/Frameworks/UIKit.framework/Headers
```

### Kotlin version

The tool picks the Kotlin/Native distribution in this order:

1. `--konan-home <path>`.
2. The `KONAN_HOME` environment variable.
3. `--kotlin-version <version>`, or the version in a coordinate such as `kn.platform:UIKit:2.4.10`.
4. `kotlin = "..."` in `gradle/libs.versions.toml` of `--project` (default: the current directory).
5. The newest distribution under `~/.konan`, or under `KONAN_DATA_DIR` when it is set.

In a project whose version catalog says `kotlin = "2.4.20"`:

```console
$ ksrc-native where kn.platform:UIKit
~/.konan/kotlin-native-prebuilt-macos-aarch64-2.4.20/klib/platform/ios_arm64/org.jetbrains.kotlin.native.platform.UIKit

$ ksrc-native where kn.platform:UIKit --kotlin-version 2.4.10
~/.konan/kotlin-native-prebuilt-macos-aarch64-2.4.10/klib/platform/ios_arm64/org.jetbrains.kotlin.native.platform.UIKit
```

When the project's version is not installed, the tool warns on stderr and uses
the newest one.

### Cache and `clean`

The cache is in `~/Library/Caches/ksrc-native` on macOS and in
`~/.cache/ksrc-native` on Linux. `KSRC_NATIVE_CACHE_DIR` moves it. Each Kotlin
version, target and framework has its own entry, so a Kotlin upgrade never
reads a stale dump.

`clean` lists the entries with their sizes. It deletes them after a yes typed
in a terminal, or at once with `-y`. Without a terminal and without `-y` it
deletes nothing.

```console
$ ksrc-native clean --dry-run
kn.platform:CoreLocation:2.4.21 (ios_arm64) 183.4 kB
...
kn.platform:Foundation:2.4.21 (ios_arm64) 2.6 MB
kn.platform:UIKit:2.4.21 (ios_arm64) 5.9 MB
...
kn.stdlib:native:2.4.21 4.9 MB
total: 8 entries, 14.3 MB in ~/Library/Caches/ksrc-native
dry run: nothing deleted
```

### Errors

An error goes to stderr as `E_CODE: message. Try: hint`, and the exit code is 1.
A search that finds nothing is not an error: it prints a note to stderr and
exits with 0.

```console
$ ksrc-native search 'x' --module kn.platform:UIKitt
E_NO_SOURCES: no platform library "UIKitt" for target ios_arm64 in Kotlin/Native 2.4.21. Try: did you mean UIKit?

$ ksrc-native search 'NoSuchSymbolAnywhere' --module kn.platform:UIKit
ksrc-native: no matches for "NoSuchSymbolAnywhere" in kn.platform:UIKit:2.4.21 (ios_arm64)
```

Ctrl-C stops a running command together with the `klib` process and the JVM
it started, and the command ends with `E_CANCELLED`. A program that runs past
its deadline (120 s for one dump) is stopped the same way, with `E_TIMEOUT`.
Nothing half built stays in the cache.

[docs/cli-api.md](docs/cli-api.md) has every command, flag and error code.

## Maintenance

- **Update the tool.** Run the install command again. `ksrc-native --version` prints the version.
- **After a Kotlin or Xcode update.** Nothing to do. A new Kotlin version gets
  new cache entries on first use. Headers are read from the selected Xcode each time.
- **Free disk space.** `ksrc-native clean -y` deletes the cache. The next search rebuilds what it needs.
- **Something is off.** `ksrc-native doctor` checks `rg`, the distribution, the `klib` tool, the JVM that `klib` runs on, the stdlib sources, Xcode and the cache. It runs `java -version` and checks that the cache directory can be written, so `ok` means the part works, not only that a file exists.

For work on the tool itself:

```bash
scripts/build.sh              # builds bin/ksrc-native with the version from VERSION
go test ./...                 # needs rg; a test of the real distribution skips itself without ~/.konan
go test -short ./...          # skips the tests that build the binary or run the real klib tool
```

- [AGENTS.md](AGENTS.md): package map, code style and the docs to keep in sync.
- [docs/cli-api.md](docs/cli-api.md): the command line, the output contract and the MCP tools.
- [docs/decisions.md](docs/decisions.md): why the tool works the way it does.
- [docs/release-workflow.md](docs/release-workflow.md): how a release is made.

## License

Apache License 2.0. See [LICENSE.txt](LICENSE.txt).

The binary contains code of the Go standard library and of a few Go modules
under BSD-3-Clause, MIT and Apache-2.0 licences. Their texts are in
[THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES), and every release archive holds
that file.
