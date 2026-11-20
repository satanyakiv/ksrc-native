package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/satanyakiv/ksrc-native/internal/testutil"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s is not a JSON object: %v", path, err)
	}
	return doc
}

// TestManifestsNameTheVersionOfTheVERSIONFile keeps the plugin manifests in
// step with the VERSION file. The release workflow runs it before it tags.
func TestManifestsNameTheVersionOfTheVERSIONFile(t *testing.T) {
	root := testutil.RepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	version := strings.TrimSpace(string(raw))
	if version == "" {
		t.Fatal("the VERSION file is empty")
	}

	claudePlugin := readJSON(t, filepath.Join(root, ".claude-plugin", "plugin.json"))
	codexPlugin := readJSON(t, filepath.Join(root, ".codex-plugin", "plugin.json"))
	claudeMarket := readJSON(t, filepath.Join(root, ".claude-plugin", "marketplace.json"))
	codexMarket := readJSON(t, filepath.Join(root, ".agents", "plugins", "marketplace.json"))

	entry := func(market map[string]any, file string) map[string]any {
		plugins, _ := market["plugins"].([]any)
		if len(plugins) != 1 {
			t.Fatalf("%s lists %d plugins, want 1", file, len(plugins))
		}
		plugin, _ := plugins[0].(map[string]any)
		return plugin
	}
	claudeEntry := entry(claudeMarket, ".claude-plugin/marketplace.json")
	codexEntry := entry(codexMarket, ".agents/plugins/marketplace.json")
	metadata, _ := claudeMarket["metadata"].(map[string]any)

	versions := map[string]any{
		".claude-plugin/plugin.json version":                 claudePlugin["version"],
		".codex-plugin/plugin.json version":                  codexPlugin["version"],
		".claude-plugin/marketplace.json metadata.version":   metadata["version"],
		".claude-plugin/marketplace.json plugins[0].version": claudeEntry["version"],
	}
	for where, got := range versions {
		if got != version {
			t.Errorf("%s = %v, want %q as in the VERSION file", where, got, version)
		}
	}

	names := map[string]any{
		".claude-plugin/plugin.json name":                  claudePlugin["name"],
		".codex-plugin/plugin.json name":                   codexPlugin["name"],
		".claude-plugin/marketplace.json plugins[0].name":  claudeEntry["name"],
		".agents/plugins/marketplace.json plugins[0].name": codexEntry["name"],
	}
	for where, got := range names {
		if got != "ksrc-native" {
			t.Errorf("%s = %v, want ksrc-native", where, got)
		}
	}

	// Both plugins load the same skill directory.
	for file, plugin := range map[string]map[string]any{".claude-plugin/plugin.json": claudePlugin, ".codex-plugin/plugin.json": codexPlugin} {
		skills, _ := plugin["skills"].(string)
		if _, err := os.Stat(filepath.Join(root, skills, "ksrc-native", "SKILL.md")); skills == "" || err != nil {
			t.Errorf("%s: skills = %q does not lead to ksrc-native/SKILL.md (%v)", file, skills, err)
		}
	}
}
