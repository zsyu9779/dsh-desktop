package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// testNodeInstall mirrors a plausible nodeInstallation for buildCommand, which
// never executes the child so the paths only need to be non-empty.
func testNodeInstall() nodeInstallation {
	return nodeInstallation{nodePath: "/usr/local/bin/node", npmPath: "/usr/local/bin/npm", version: "22.19.0"}
}

func TestBuildCommandExposesDSHWorkspace(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("DSH_WORKSPACE", ws)

	m := &dshManager{}
	cmd, err := m.buildCommand(context.Background(), 12345, testNodeInstall())
	if err != nil {
		t.Fatalf("buildCommand: %v", err)
	}

	if cmd.Dir != ws {
		t.Fatalf("cmd.Dir = %q, want %q", cmd.Dir, ws)
	}

	found := false
	for _, kv := range cmd.Env {
		if kv == "DSH_WORKSPACE="+ws {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("DSH_WORKSPACE=%s not present in child env", ws)
	}
}

func TestBuildCommandDefaultWorkspaceIsHome(t *testing.T) {
	t.Setenv("DSH_WORKSPACE", "")
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}

	m := &dshManager{}
	cmd, err := m.buildCommand(context.Background(), 12345, testNodeInstall())
	if err != nil {
		t.Fatalf("buildCommand: %v", err)
	}

	for _, kv := range cmd.Env {
		if kv == "DSH_WORKSPACE="+home {
			return
		}
	}
	t.Fatalf("DSH_WORKSPACE=%s (home default) not present in child env", home)
}

func TestFindCachedDSHExecutableRequiresPinnedCompleteDlx(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "cache-key", "install-key", "node_modules", "@deepseek-ai", "dsh")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(`{"version":"0.1.1-rc.2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "cache-key", "install-key", "node_modules", ".bin", "dsh")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := findCachedDSHExecutable([]string{root}, "0.1.1-rc.2"); got != bin {
		t.Fatalf("findCachedDSHExecutable() = %q, want %q", got, bin)
	}
	if got := findCachedDSHExecutable([]string{root}, "9.9.9"); got != "" {
		t.Fatal(fmt.Sprintf("wrong version returned %q", got))
	}
}
