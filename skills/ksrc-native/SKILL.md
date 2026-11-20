---
name: ksrc-native
description: Search/read Kotlin signatures of Kotlin/Native platform APIs (`platform.UIKit`, `platform.Foundation`, other `platform.*`). Use it instead of guessing an iOS API shape in Kotlin Multiplatform code, and instead of reading `~/.konan` directly.
---

# ksrc-native

`ksrc-native` answers "what does this Apple API look like from Kotlin?". Platform
libraries ship as klibs without sources, so the tool dumps their metadata once,
splits it into one file per class and lets you search and read those files.

## When to use

- Before you write or fix `iosMain` code that imports `platform.*`: check the
  exact Kotlin name, parameter labels, nullability and return type.
- A compile error names a `platform.*` symbol (unresolved reference, type
  mismatch, wrong overload).
- You need the members of a class, the methods of a delegate protocol, or a
  constant (`NSURLErrorDomain`, `UIApplicationOpenSettingsURLString`).

Not for library dependencies from Gradle (use `ksrc`), and not for the Kotlin
stdlib or Xcode headers in this version.

## Flow: search, then cat

1. Search a framework for a declaration. Every hit is
   `<file-id> <line>:<col>:<text>`.

   ```bash
   ksrc-native search 'class platform/UIKit/UIApplication ' --module kn.platform:UIKit
   # kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt 2:13:public open class platform/UIKit/UIApplication : platform/UIKit/UIResponder {
   ```

2. Read the file with the file-id from the hit. Quote it: it contains `!`.

   ```bash
   ksrc-native cat 'kn.platform:UIKit:2.4.21!/ios_arm64/UIApplication.kt' --lines 1,60
   ```

The first search of a framework takes a few seconds (the dump is built and
cached); later searches are instant. Without `--module`, search covers the
frameworks that are already cached.

## How to write patterns

Names in a dump are fully qualified with slashes: `platform/UIKit/UIView`,
`kotlin/String`, `kotlinx/cinterop/CPointer`. Patterns are ripgrep regular
expressions.

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

## Flags

- `--module kn.platform:<Framework>[:<kotlinVersion>]` (repeatable, short `-m`).
  The framework is the name after `import platform.`.
- `--target <target>`: default `ios_arm64`; use `ios_simulator_arm64`,
  `macos_arm64` and so on when the API differs per target.
- `--context N` (short `-C`): lines around each match. Context lines print as
  `<file-id> <line>-<text>`.
- `-- <rg-args>`: extra ripgrep arguments, for example `-- -i`, `-- -F`,
  `-- -g '*Delegate*'`. Put them last.
- `cat --lines A,B` (1-based, inclusive), `cat -n` for line numbers.
- Kotlin version: read from `gradle/libs.versions.toml` of `--project`
  (default: the current directory). Override with `--kotlin-version` or
  `--konan-home`.

Aliases: `ksrc-native rg` is `search`, `ksrc-native open` is `cat`.

## Common errors

Errors go to stderr as `E_CODE: message. Try: hint`. Follow the hint.

- `E_NO_KONAN`: no Kotlin/Native distribution in `~/.konan`. Run a Kotlin/Native
  Gradle task once (for example `./gradlew compileKotlinIosArm64`), or pass
  `--konan-home`.
- `E_NO_SOURCES`: unknown framework or target, or a search without `--module`
  before anything is cached. Check the name in the hint.
- `E_NO_RG`: install ripgrep.
- `E_KLIB_FAILED`: the `klib` tool needs a JDK on `PATH`.
- `E_NO_FILE`, `E_BAD_FILE_ID`: copy the file-id from a fresh search line.
- `no matches` on stderr with exit code 0: the pattern found nothing. Loosen it
  (drop the package, add `-- -i`) or try another framework.

`ksrc-native doctor` shows what the tool found: `rg`, the distribution, the
`klib` tool and the cache.
