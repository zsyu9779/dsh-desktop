package main

import (
	"context"
	"os"
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
