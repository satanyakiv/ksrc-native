package konan

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CompareVersions orders Kotlin versions. It returns a negative number when
// a < b, zero when they are equal and a positive number when a > b.
// A release is newer than its pre-releases: 2.5.0 > 2.5.0-RC2 > 2.5.0-Beta1.
func CompareVersions(a, b string) int {
	aCore, aPre, _ := strings.Cut(a, "-")
	bCore, bPre, _ := strings.Cut(b, "-")

	aParts := strings.Split(aCore, ".")
	bParts := strings.Split(bCore, ".")
	for i := 0; i < len(aParts) || i < len(bParts); i++ {
		if c := compareNumeric(part(aParts, i), part(bParts, i)); c != 0 {
			return c
		}
	}

	switch {
	case aPre == bPre:
		return 0
	case aPre == "":
		return 1
	case bPre == "":
		return -1
	default:
		return strings.Compare(aPre, bPre)
	}
}

func part(parts []string, i int) string {
	if i < len(parts) {
		return parts[i]
	}
	return "0"
}

func compareNumeric(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	if aErr != nil || bErr != nil {
		return strings.Compare(a, b)
	}
	switch {
	case an < bn:
		return -1
	case an > bn:
		return 1
	default:
		return 0
	}
}

// ProjectKotlinVersion reads the `kotlin = "..."` entry of the [versions]
// table in <projectDir>/gradle/libs.versions.toml. Only the plain string form
// is understood; anything else reports false.
func ProjectKotlinVersion(projectDir string) (string, bool) {
	f, err := os.Open(filepath.Join(projectDir, "gradle", "libs.versions.toml"))
	if err != nil {
		return "", false
	}
	defer f.Close()

	inVersions := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inVersions = strings.TrimSpace(strings.Trim(line, "[]")) == "versions"
			continue
		}
		if !inVersions {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.Trim(strings.TrimSpace(key), `"'`) != "kotlin" {
			continue
		}
		return tomlString(value)
	}
	return "", false
}

// tomlString parses a basic or literal one-line TOML string and ignores a
// trailing comment.
func tomlString(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return "", false
	}
	quote := value[0]
	if quote != '"' && quote != '\'' {
		return "", false
	}
	end := strings.IndexByte(value[1:], quote)
	if end <= 0 {
		return "", false
	}
	return value[1 : 1+end], true
}
