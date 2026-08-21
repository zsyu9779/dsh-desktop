package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionPattern(t *testing.T) {
	valid := []string{"0.1.1-rc.2", "1.2.3", "0.1.0-rc.8", "2.0.0-beta.1", "0.1.1-rc"}
	invalid := []string{"", "v0.1.1", "0.1", "abc", "@deepseek-ai/dsh@0.1.1-rc.2", "0.1.1 rc.2", "0.1.1-rc.2 "}
	for _, v := range valid {
		if !dshVersionPattern.MatchString(v) {
			t.Errorf("expected %q to match version pattern", v)
		}
	}
	for _, v := range invalid {
		if dshVersionPattern.MatchString(v) {
			t.Errorf("expected %q NOT to match version pattern", v)
		}
	}
}

func TestPinnedDSHPackageUsesConfigOverride(t *testing.T) {
	dir := t.TempDir()
	oldPath := configPath
	oldStateDir := os.Getenv(stateDirEnv)
	t.Cleanup(func() {
		configPath = oldPath
		os.Setenv(stateDirEnv, oldStateDir)
	})
	os.Setenv(stateDirEnv, dir)
	configPath = filepath.Join(stateDir(), "config.json")

	// No config file -> compiled-in default.
	if got := pinnedDSHPackage(); got != dshPackage {
		t.Fatalf("expected default pin %q, got %q", dshPackage, got)
	}
	if got := currentDSHVersion(); got != "0.1.1-rc.2" {
		t.Fatalf("expected current version 0.1.1-rc.2, got %q", got)
	}

	// Config override wins.
	if err := saveDesktopConfig(desktopConfig{DSHVersion: "9.9.9"}); err != nil {
		t.Fatal(err)
	}
	if got := pinnedDSHPackage(); got != "@deepseek-ai/dsh@9.9.9" {
		t.Fatalf("expected override pin, got %q", got)
	}
	if got := currentDSHVersion(); got != "9.9.9" {
		t.Fatalf("expected current version 9.9.9, got %q", got)
	}

	// Malformed config -> falls back to default.
	if err := os.WriteFile(configPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := pinnedDSHPackage(); got != dshPackage {
		t.Fatalf("expected fallback to default pin after malformed config, got %q", got)
	}
}

func TestSetDSHVersionValidation(t *testing.T) {
	dir := t.TempDir()
	oldPath := configPath
	oldStateDir := os.Getenv(stateDirEnv)
	t.Cleanup(func() {
		configPath = oldPath
		os.Setenv(stateDirEnv, oldStateDir)
	})
	os.Setenv(stateDirEnv, dir)
	configPath = filepath.Join(stateDir(), "config.json")

	a := NewApp() // nil ctx: no events emitted
	if err := a.SetDSHVersion("not-a-version"); err == nil {
		t.Fatal("expected error for invalid version")
	}
	if err := a.SetDSHVersion("1.2.3"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := loadDesktopConfig()
	if cfg.DSHVersion != "1.2.3" {
		t.Fatalf("expected config to persist 1.2.3, got %q", cfg.DSHVersion)
	}
}
