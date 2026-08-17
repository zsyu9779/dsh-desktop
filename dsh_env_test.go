package main

import (
	"context"
	"os"
	"testing"
)

func TestBuildCommandExposesDSHWorkspace(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("DSH_WORKSPACE", ws)

	m := &dshManager{}
	cmd, err := m.buildCommand(context.Background(), 12345)
	if err != nil {
		// npx may be absent in some environments; that's not what this test asserts.
		if _, lookErr := findExecutable("npx"); lookErr != nil {
			t.Skipf("npx not available: %v", lookErr)
		}
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
	cmd, err := m.buildCommand(context.Background(), 12345)
	if err != nil {
		if _, lookErr := findExecutable("npx"); lookErr != nil {
			t.Skipf("npx not available: %v", lookErr)
		}
		t.Fatalf("buildCommand: %v", err)
	}

	for _, kv := range cmd.Env {
		if kv == "DSH_WORKSPACE="+home {
			return
		}
	}
	t.Fatalf("DSH_WORKSPACE=%s (home default) not present in child env", home)
}
