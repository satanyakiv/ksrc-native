// Package konan locates a Kotlin/Native distribution and the things inside it
// that the tool reads: platform klibs, their manifests and the klib tool.
package konan

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

// Distribution is one Kotlin/Native distribution on disk.
type Distribution struct {
	Home    string // absolute path, for example ~/.konan/kotlin-native-prebuilt-macos-aarch64-2.4.21
	Version string // Kotlin version, for example 2.4.21; "unknown" when it cannot be read
}

// UnknownVersion is the Version of a distribution whose version cannot be read.
const UnknownVersion = "unknown"

// DiscoverOptions are the inputs of Discover. Empty fields are skipped.
type DiscoverOptions struct {
	KonanHome     string              // --konan-home
	KotlinVersion string              // --kotlin-version, or the version of a coordinate
	ProjectDir    string              // --project; its gradle/libs.versions.toml is read
	Getenv        func(string) string // os.Getenv in production
	UserHome      string              // the user's home directory, parent of .konan
	Warn          io.Writer           // receives non-fatal diagnostics; may be nil
}

const installHint = "run a Kotlin/Native Gradle task once (for example ./gradlew compileKotlinIosArm64) so Gradle downloads the distribution, or pass --konan-home <path>"

// Discover finds the distribution to use. The order is: KonanHome, the
// KONAN_HOME variable, KotlinVersion, the Kotlin version of the project's
// version catalog, and at last the newest installed distribution.
func Discover(o DiscoverOptions) (Distribution, error) {
	getenv := o.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	if o.KonanHome != "" {
		return explicitHome(o.KonanHome, "--konan-home", o.KotlinVersion)
	}
	if env := getenv("KONAN_HOME"); env != "" {
		return explicitHome(env, "KONAN_HOME", o.KotlinVersion)
	}

	dataDir := DataDir(getenv, o.UserHome)
	installed := Installed(dataDir)

	if o.KotlinVersion != "" {
		if d, ok := findVersion(installed, o.KotlinVersion); ok {
			return d, nil
		}
		return Distribution{}, errs.New(errs.NoKonan,
			fmt.Sprintf("no Kotlin/Native %s distribution in %s (installed: %s)", o.KotlinVersion, dataDir, versionList(installed)),
			installHint)
	}

	if o.ProjectDir != "" {
		if v, ok := ProjectKotlinVersion(o.ProjectDir); ok {
			if d, ok := findVersion(installed, v); ok {
				return d, nil
			}
			if len(installed) > 0 && o.Warn != nil {
				// Like every note of the tool, so a reader of stderr can tell
				// it from the output of another program.
				fmt.Fprintf(o.Warn, "ksrc-native: the project uses Kotlin %s, but its Kotlin/Native distribution is not in %s; using %s\n",
					v, dataDir, installed[0].Version)
			}
		}
	}

	if len(installed) == 0 {
		return Distribution{}, errs.New(errs.NoKonan,
			fmt.Sprintf("no Kotlin/Native distribution found in %s", dataDir), installHint)
	}
	return installed[0], nil
}

// DataDir returns the directory that holds downloaded distributions:
// KONAN_DATA_DIR when set, else <userHome>/.konan.
func DataDir(getenv func(string) string, userHome string) string {
	if getenv != nil {
		if dir := getenv("KONAN_DATA_DIR"); dir != "" {
			return dir
		}
	}
	return filepath.Join(userHome, ".konan")
}

// distPrefixes are the directory name prefixes of a distribution. Kotlin 2.x
// downloads "kotlin-native-prebuilt-<os>-<arch>-<version>"; older releases
// dropped the "prebuilt" part.
var distPrefixes = []string{"kotlin-native-prebuilt-", "kotlin-native-"}

// Installed lists the distributions in dataDir, newest first.
func Installed(dataDir string) []Distribution {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil
	}
	var out []Distribution
	for _, e := range entries {
		version, ok := versionFromDirName(e.Name())
		if !ok {
			continue
		}
		home := filepath.Join(dataDir, e.Name())
		if !looksLikeHome(home) {
			continue
		}
		out = append(out, Distribution{Home: home, Version: version})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return CompareVersions(out[i].Version, out[j].Version) > 0
	})
	return out
}

// versionFromDirName extracts "2.4.21" from
// "kotlin-native-prebuilt-macos-aarch64-2.4.21".
func versionFromDirName(name string) (string, bool) {
	for _, prefix := range distPrefixes {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		// rest is "<os>-<arch>-<version>"; the version itself may hold dashes.
		parts := strings.SplitN(rest, "-", 3)
		if len(parts) == 3 && parts[2] != "" && parts[2][0] >= '0' && parts[2][0] <= '9' {
			return parts[2], true
		}
	}
	return "", false
}

func looksLikeHome(home string) bool {
	info, err := os.Stat(filepath.Join(home, "klib"))
	return err == nil && info.IsDir()
}

func explicitHome(path, source, wantVersion string) (Distribution, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if !looksLikeHome(abs) {
		return Distribution{}, errs.New(errs.NoKonan,
			fmt.Sprintf("%s points at %s, which is not a Kotlin/Native distribution (no klib directory)", source, abs),
			"point it at a directory like ~/.konan/kotlin-native-prebuilt-macos-aarch64-<version>")
	}
	d := Distribution{Home: abs, Version: homeVersion(abs)}
	if wantVersion != "" && d.Version != UnknownVersion && d.Version != wantVersion {
		return Distribution{}, errs.New(errs.NoKonan,
			fmt.Sprintf("Kotlin %s was requested, but %s points at a %s distribution", wantVersion, source, d.Version),
			"drop "+source+" or request version "+d.Version)
	}
	return d, nil
}

// homeVersion reads the version of a distribution at an arbitrary path: from
// the directory name first, then from the manifest of a platform klib.
func homeVersion(home string) string {
	if v, ok := versionFromDirName(filepath.Base(home)); ok {
		return v
	}
	matches, _ := filepath.Glob(filepath.Join(home, "klib", "platform", "*", "*", "default", "manifest"))
	sort.Strings(matches)
	for _, m := range matches {
		if props, err := readProperties(m); err == nil && props["compiler_version"] != "" {
			return props["compiler_version"]
		}
	}
	return UnknownVersion
}

func findVersion(installed []Distribution, version string) (Distribution, bool) {
	for _, d := range installed {
		if d.Version == version {
			return d, true
		}
	}
	return Distribution{}, false
}

func versionList(installed []Distribution) string {
	if len(installed) == 0 {
		return "none"
	}
	versions := make([]string, len(installed))
	for i, d := range installed {
		versions[i] = d.Version
	}
	return strings.Join(versions, ", ")
}
