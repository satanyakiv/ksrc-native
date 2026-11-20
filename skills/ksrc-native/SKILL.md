---
name: ksrc-native
description: Search/read what Gradle dependency search does not reach in Kotlin Multiplatform iOS code - Kotlin signatures of Kotlin/Native platform APIs (`platform.UIKit`, `platform.Foundation`, other `platform.*`), sources of the native stdlib and `kotlinx.cinterop`, and Objective-C headers of the Xcode SDK. Use it instead of guessing an iOS API shape, and instead of reading `~/.konan` or Xcode directories directly.
---

# ksrc-native

`ksrc-native` answers "what does this Apple API look like from Kotlin?" and
"what does the native stdlib or the Objective-C header say?". It searches three
kinds of modules:

| Module | Content | First use |
| --- | --- | --- |
| `kn.platform:<Framework>[:<kotlinVersion>]` | Kotlin signatures of `platform.<Framework>`, one file per class | builds a dump, a few seconds |
| `kn.stdlib:native[:<kotlinVersion>]` | sources of the native stdlib and `kotlinx.cinterop` | extracts a zip, under a second |
| `sdk.apple:<Framework>[:<sdk>]` | Objective-C headers of an Xcode SDK, with their doc comments | nothing to build |

## When to use

- Before you write or fix `iosMain` code that imports `platform.*`: check the
  exact Kotlin name, parameter labels, nullability and return type.
- A compile error names a `platform.*` symbol (unresolved reference, type
  mismatch, wrong overload).
- You need the members of a class, the methods of a delegate protocol, or a
  constant (`NSURLErrorDomain`, `UIApplicationOpenSettingsURLString`).
- You need the body or the KDoc of a `kotlinx.cinterop` or native stdlib
  function (`memScoped`, `usePinned`, `autoreleasepool`).
- A Kotlin signature is not enough: you need Apple's comment, the availability
  macro or the nullability annotation from the header.

Not for library dependencies from Gradle (use `ksrc`).

## Flow: search, then cat

1. Search a module for a declaration. Every hit is
   `<file-id> <line>:<col>:<text>`.

   ```bash
   ksrc-native search 'class platform/UIKit/UIApplication ' --module kn.platform:UIKit
   # kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt 2:13:public open class platform/UIKit/UIApplication : platform/UIKit/UIResponder {
   ```

2. Read the file with the file-id from the hit. Quote it: it contains `!`.

   ```bash
   ksrc-native cat 'kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt' --lines 1,60
   ```

The same flow works for the other modules:

```bash
ksrc-native search 'fun <R> memScoped' --module kn.stdlib:native
# kn.stdlib:native:2.4.21!/nativeMain/kotlinx/cinterop/Utils.kt 715:15:public inline fun <R> memScoped(block: MemScope.()->R): R {

ksrc-native search 'presentViewController:' --module sdk.apple:UIKit -- -F
# sdk.apple:UIKit:iphoneos26.4!/UIViewController.h 262:9:- (void)presentViewController:(UIViewController *)viewControllerToPresent ...
```

Without `--module`, search covers the platform frameworks that are already
cached, plus the stdlib. It never builds all frameworks unasked;
`--all-platform` does that (minutes the first time).

## How to write patterns

Patterns are ripgrep regular expressions. A pattern cannot be empty. On the
command line it cannot start with `-`: write `'[-]foo'` in place of `'-foo'`.

**Platform dumps.** Names are fully qualified with slashes:
`platform/UIKit/UIView`, `kotlin/String`, `kotlinx/cinterop/CPointer`.

| You look for | Pattern | Where it lands |
| --- | --- | --- |
| a class | `'class platform/UIKit/UIApplication '` | `UIApplication.kt` |
| a protocol (Kotlin adds `Protocol`) | `'interface platform/WebKit/WKNavigationDelegateProtocol '` | `WKNavigationDelegateProtocol.kt` |
| a method by Kotlin name | `'fun openURL\('` | the class file |
| a method by Objective-C selector | `'selector = "openURL:options:completionHandler:"'` | the line above the `fun` |
| class (static) methods | `'fun sharedApplication'` | `UIApplicationMeta.kt` |
| a constant, global function, type alias | `'val NSURLErrorDomain:'` | `_toplevel.kt` |

Limit a search to one class with an rg glob: `-- -g UIApplication.kt`.
A companion object and nested classes live in the file of their outer class.

A few classes live in a library with another name than their package. In
Kotlin 2.4.21 `platform/CoreLocation/CLLocation` is in
`kn.platform:_LocationEssentials`, not in `kn.platform:CoreLocation`. When a
class is not in the framework its package names, look at the libraries whose
name starts with `_` in `ksrc-native deps`, or search with `--all-platform`.

**Stdlib.** Ordinary Kotlin sources: `'fun <R> memScoped'`, `'class Arena'`.
Native-only code is under `nativeMain/`, shared code under `commonMain/`.

**Headers.** Objective-C: search the selector with `-- -F`
(`'presentViewController:animated:'`), a class with `'@interface UIViewController'`,
a protocol with `'@protocol UITableViewDelegate'`. Swift-only frameworks
(SwiftData, Charts) have no headers and no `platform.*` library.

## Other commands

- `ksrc-native deps`: the modules that can be searched, with the cache state.
- `ksrc-native where <coordinate>`: the klib, zip or Headers directory behind a
  module. It builds nothing.
- `ksrc-native doctor`: what the tool found (`rg`, the distribution, the `klib`
  tool, the JVM, the stdlib sources, Xcode, the cache). It runs `java -version`
  and checks that the cache can be written; a `MISSING` line says what to do.
- `ksrc-native clean`: lists the cache with sizes. It deletes only with `-y`,
  or after a yes typed in a terminal; `--dry-run` never deletes.

## MCP

`ksrc-native mcp` serves the same operations as MCP tools over stdio: `search`,
`cat` and `deps` by default. `--tools` replaces that list:
`--tools=search,cat,deps,where` or `--tools=all` serves `where` too. A tool takes the
command's flags as inputs in snake_case (`pattern`, `modules`, `rg_args`,
`file_id`, `lines`, `target`, `sdk`) and returns what the command prints. A
failure is a tool result with `isError` and the `E_CODE: message. Try: hint`
line. There is no `konan_home` input: the distribution is chosen when the
server starts (`ksrc-native --konan-home <path> mcp`). Pick a Kotlin version
with `kotlin_version` or `project`.

One result holds at most 64 KiB of text (the server's `--max-result-bytes`
changes that). A longer result ends with a line that starts with
`ksrc-native: output truncated`. The hits after it are missing: narrow the
pattern, name one module, or read a file with `cat` and `lines`.

## Flags

- `--module <coordinate>` (repeatable, short `-m`). For `kn.platform` the name
  is what follows `import platform.`.
- `--target <target>`: default `ios_arm64`, or the host target (`linux_x64`)
  in a distribution without it; use `ios_simulator_arm64`,
  `macos_arm64` and so on when the platform API differs per target. Write the
  name exactly, in lower case; a wrong one is `E_NO_SOURCES` with the list.
- `--sdk <sdk>`: Apple SDK of `sdk.apple` modules; default `iphoneos`. Also
  `iphonesimulator`, `macosx`, or a full name from `deps` such as
  `iphoneos26.4`. An SDK in the coordinate wins over the flag. It is a name,
  never a path.
- `--context N` (short `-C`): lines around each match. Context lines print as
  `<file-id> <line>-<text>`.
- `-- <rg-args>`: extra ripgrep arguments, for example `-- -i`, `-- -F`,
  `-- -g '*Delegate*'`. Put them last. Only these pass: `-i -s -S -F -w -x -v
  -U -P --multiline-dotall`, `-g/--glob <glob>`, `--iglob <glob>`,
  `-m/--max-count <n>`, `-A/--after-context <n>`, `-B/--before-context <n>`,
  `--max-columns <n>`. A path or any other flag is `E_BAD_ARGS`. For context
  lines use `--context N` before the `--`.
- `cat --lines A,B` (1-based, inclusive); `A,` runs to the end of the file
  and `A` is one line; an end past the last line is cut to it. `cat -n`
  prints line numbers.
- Kotlin version: read from `gradle/libs.versions.toml` of `--project`
  (default: the current directory). Override with `--kotlin-version` or
  `--konan-home`.

Aliases: `ksrc-native rg` is `search`, `ksrc-native open` is `cat`.

## Common errors

Errors go to stderr as `E_CODE: message. Try: hint`, and the exit code is 1.
Follow the hint.

- `E_NO_KONAN`: no Kotlin/Native distribution in `~/.konan`. Run a Kotlin/Native
  Gradle task once (for example `./gradlew compileKotlinIosArm64`), or pass
  `--konan-home`. `sdk.apple` modules work without one.
- `E_NO_XCODE`: `xcrun` found no developer tools. Only `sdk.apple` modules need
  Xcode, and they exist on macOS only.
- `E_NO_SOURCES`: unknown framework, target or SDK, a Swift-only framework, or
  a search without `--module` when nothing is available. Check the name in the
  hint, or run `ksrc-native deps`.
- `E_NO_RG`: install ripgrep.
- `E_BAD_ARGS` that names an rg argument: the argument after `--` (or in
  `rg_args`) is not on the list above. Drop it; the hint repeats the list.
- `E_KLIB_FAILED`: the `klib` tool failed. It runs on a JDK: run
  `ksrc-native doctor` and read the `java` line.
- `E_TIMEOUT`: a program ran past its deadline (`klib` 120 s, `rg` 60 s,
  `xcrun` 15 s) and was stopped. Run the command once more; for `rg`, narrow
  the search with `--module`.
- `E_CANCELLED`: the run was interrupted. Nothing is left half-built; run it
  again.
- `E_NO_FILE`, `E_BAD_FILE_ID`: copy the file-id from a fresh search line.
- `no matches` on stderr with exit code 0: the pattern found nothing. Loosen it
  (drop the package, add `-- -i`) or try another module.
