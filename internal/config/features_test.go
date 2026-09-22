package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartupRejectsP0WithLegacyGamificationImport(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "invalid-features.yaml")
	configYAML := "app:\n  env: testing\n  node_id: 1\nfeatures:\n  p0: true\n  legacy_gamification_import: true\nlog:\n  level: info\n  output: stdout\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "CONFIG_PATH="+configPath)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("invalid feature combination was accepted: %s", output)
	}
	if !strings.Contains(string(output), "P0 and legacy gamification import are mutually exclusive") {
		t.Fatalf("startup rejected configuration without the feature validation error: %s", output)
	}
}
