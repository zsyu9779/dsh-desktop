package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noopLogf(string, ...any) {}

func TestResolveDSHHome(t *testing.T) {
	t.Setenv("DSH_HOME", "/custom/dsh")
	if got := resolveDSHHome(); got != "/custom/dsh" {
		t.Fatalf("resolveDSHHome with DSH_HOME = %q, want /custom/dsh", got)
	}

	t.Setenv("DSH_HOME", "")
	got := resolveDSHHome()
	if got == "" || !strings.HasSuffix(got, ".dsh") {
		t.Fatalf("resolveDSHHome default = %q, want a path ending in .dsh", got)
	}
}

func TestPatchHasPlugin(t *testing.T) {
	content := "- insert:\n    - id: file-changes\n      name: dsh-file-changes\n"
	if !patchHasPlugin(content, "file-changes") {
		t.Error("patchHasPlugin should find file-changes")
	}
	if patchHasPlugin(content, "diff-review") {
		t.Error("patchHasPlugin should not find diff-review")
	}
	// No false positive on an id that only shares a prefix.
	if patchHasPlugin("- insert:\n    - id: diff-review-extra\n", "diff-review") {
		t.Error("patchHasPlugin should not match diff-review-extra for id diff-review")
	}
}

func TestRunPreinstallInstallsAndIsIdempotent(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)
	t.Setenv(stateDirEnv, t.TempDir())

	status, err := runPreinstall(noopLogf)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	wantStatus := fmt.Sprintf("installed %d", len(preinstalledPlugins))
	if !strings.Contains(status, wantStatus) {
		t.Fatalf("first run status = %q, want %q", status, wantStatus)
	}

	pkg := filepath.Join(dshHome, "profiles", "node_modules", "dsh-file-changes", "package.json")
	if _, err := os.Stat(pkg); err != nil {
		t.Fatalf("plugin not copied: %v", err)
	}

	patchPath := filepath.Join(dshHome, "profiles", "web", "cordis.patch.yml")
	raw, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("patch file missing: %v", err)
	}
	if !strings.Contains(string(raw), "id: file-changes") {
		t.Fatalf("patch missing insert block: %s", raw)
	}

	// Second run must be a no-op and must not duplicate the patch.
	status2, err := runPreinstall(noopLogf)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !strings.Contains(status2, "up to date") {
		t.Fatalf("second run status = %q, want up to date", status2)
	}
	raw2, _ := os.ReadFile(patchPath)
	if strings.Count(string(raw2), "id: file-changes") != 1 {
		t.Fatalf("patch insert duplicated: %s", raw2)
	}
}

func TestRunPreinstallUpgradesOnVersionBump(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)
	t.Setenv(stateDirEnv, t.TempDir())

	if _, err := runPreinstall(noopLogf); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dshHome, "profiles", "node_modules", "dsh-file-changes")

	// Simulate an older recorded version for file-changes and a corrupted install.
	var wantVersion string
	state := preinstallState{SchemaVersion: 1}
	for _, p := range preinstalledPlugins {
		v := p.Version
		if p.Name == "dsh-file-changes" {
			wantVersion = p.Version
			v = "0.0.0"
		}
		state.Installed = append(state.Installed, installedPlugin{Name: p.Name, Version: v})
	}
	if err := savePreinstallState(preinstallStatePath(), state); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(target, "package.json")); err != nil {
		t.Fatal(err)
	}

	status, err := runPreinstall(noopLogf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "installed 1") {
		t.Fatalf("upgrade status = %q, want installed 1", status)
	}
	if _, err := os.Stat(filepath.Join(target, "package.json")); err != nil {
		t.Fatalf("package.json not restored after upgrade: %v", err)
	}

	s := loadPreinstallState(preinstallStatePath())
	for _, ip := range s.Installed {
		if ip.Name == "dsh-file-changes" && ip.Version == wantVersion {
			return
		}
	}
	t.Fatalf("state not updated to current version %q: %+v", wantVersion, s.Installed)
}

func TestUninstallPreinstalledPlugin(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)
	t.Setenv(stateDirEnv, t.TempDir())

	if _, err := runPreinstall(noopLogf); err != nil {
		t.Fatal(err)
	}

	if err := uninstallPreinstalledPlugin("file-changes", noopLogf); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	// Directory removed.
	target := filepath.Join(dshHome, "profiles", "node_modules", "dsh-file-changes")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("plugin dir still present after uninstall")
	}
	// Patch block removed.
	raw, _ := os.ReadFile(filepath.Join(dshHome, "profiles", "web", "cordis.patch.yml"))
	if strings.Contains(string(raw), "id: file-changes") {
		t.Fatalf("patch block still present: %s", raw)
	}
	// Other plugins remain registered.
	if !strings.Contains(string(raw), "id: diff-review") {
		t.Fatalf("other plugin lost after uninstall: %s", raw)
	}
	// State no longer records it.
	s := loadPreinstallState(preinstallStatePath())
	for _, ip := range s.Installed {
		if ip.Name == "dsh-file-changes" {
			t.Fatalf("state still records uninstalled plugin: %+v", s.Installed)
		}
	}

	// Unknown id is an error, not a panic.
	if err := uninstallPreinstalledPlugin("nope", noopLogf); err == nil {
		t.Fatal("expected error for unknown plugin id")
	}
}

func TestRunPreinstallLeavesUserInstallUntouched(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)
	t.Setenv(stateDirEnv, t.TempDir())

	target := filepath.Join(dshHome, "profiles", "node_modules", "dsh-file-changes")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	userPkg := `{"name":"user-installed"}`
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(userPkg), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runPreinstall(noopLogf); err != nil {
		t.Fatalf("runPreinstall: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(target, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != userPkg {
		t.Fatalf("user install overwritten: %s", raw)
	}

	// The patch layer must not be appended for a user-owned plugin either.
	patchPath := filepath.Join(dshHome, "profiles", "web", "cordis.patch.yml")
	if _, err := os.Stat(patchPath); !os.IsNotExist(err) {
		raw, _ := os.ReadFile(patchPath)
		if strings.Contains(string(raw), "id: file-changes") {
			t.Fatalf("user-owned plugin still got a patch insert: %s", raw)
		}
	}
}
