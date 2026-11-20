package konan

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/satanyakiv/ksrc-native/internal/errs"
)

// DefaultTarget is the Kotlin/Native target used when --target is not given.
const DefaultTarget = "ios_arm64"

// platformKlibPrefix starts the directory name of every platform klib.
const platformKlibPrefix = "org.jetbrains.kotlin.native.platform."

// Targets lists the targets that have platform klibs, sorted by name.
func (d Distribution) Targets() []string {
	return dirNames(filepath.Join(d.Home, "klib", "platform"), "")
}

// Frameworks lists the platform libraries of a target (UIKit, Foundation,
// posix, ...), sorted by name. It is empty for an unknown target.
func (d Distribution) Frameworks(target string) []string {
	return dirNames(filepath.Join(d.Home, "klib", "platform", target), platformKlibPrefix)
}

// PlatformKlib describes one platform library of a distribution.
type PlatformKlib struct {
	Framework string // canonical name, for example UIKit
	Target    string // for example ios_arm64
	Path      string // the klib directory
}

// FindPlatformKlib locates the platform klib of a framework. The framework
// name is matched without regard to case, and the canonical spelling is
// returned.
func (d Distribution) FindPlatformKlib(target, framework string) (PlatformKlib, error) {
	frameworks := d.Frameworks(target)
	if len(frameworks) == 0 {
		return PlatformKlib{}, errs.New(errs.NoSources,
			fmt.Sprintf("Kotlin/Native %s has no platform libraries for target %q", d.Version, target),
			"pass --target with one of: "+strings.Join(d.Targets(), ", "))
	}
	for _, name := range frameworks {
		if strings.EqualFold(name, framework) {
			return PlatformKlib{
				Framework: name,
				Target:    target,
				Path:      filepath.Join(d.Home, "klib", "platform", target, platformKlibPrefix+name),
			}, nil
		}
	}
	return PlatformKlib{}, errs.New(errs.NoSources,
		fmt.Sprintf("no platform library %q for target %s in Kotlin/Native %s", framework, target, d.Version),
		suggestFrameworks(framework, frameworks))
}

// suggestFrameworks builds the hint for an unknown framework name.
func suggestFrameworks(want string, frameworks []string) string {
	const limit = 8
	lower := strings.ToLower(want)
	var near []string
	for _, name := range frameworks {
		n := strings.ToLower(name)
		if strings.Contains(n, lower) || strings.Contains(lower, n) {
			near = append(near, name)
		}
	}
	if len(near) == 0 {
		return fmt.Sprintf("use a framework name as in `import platform.<Framework>` (%d are available, for example Foundation, UIKit, CoreGraphics)", len(frameworks))
	}
	if len(near) > limit {
		near = near[:limit]
	}
	return "did you mean " + strings.Join(near, ", ") + "?"
}

// KlibTool returns the path of the klib command-line tool of the distribution.
func (d Distribution) KlibTool() string {
	return filepath.Join(d.Home, "bin", "klib")
}

// Manifest reads the manifest of a klib directory as key-value pairs.
func Manifest(klibDir string) (map[string]string, error) {
	return readProperties(filepath.Join(klibDir, "default", "manifest"))
}

// readProperties parses a Java-style properties file of the simple
// "key=value" kind that klib manifests use.
func readProperties(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	props := make(map[string]string)
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			if key, value, ok := strings.Cut(line, "="); ok {
				props[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
		if err != nil {
			break
		}
	}
	return props, nil
}

// dirNames lists the subdirectories of dir whose names start with prefix, with
// the prefix removed, sorted.
func dirNames(dir, prefix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if name, ok := strings.CutPrefix(e.Name(), prefix); ok && name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
