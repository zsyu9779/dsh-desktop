package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

func TestPreinstalledClientBundlesRegisterPackageName(t *testing.T) {
	for _, plugin := range preinstalledPlugins {
		t.Run(plugin.Name, func(t *testing.T) {
			packageJSON, err := fs.ReadFile(pluginsFS, filepath.ToSlash(filepath.Join("plugins", plugin.Dir, "package.json")))
			if err != nil {
				t.Fatal(err)
			}

			var manifest struct {
				Exports map[string]json.RawMessage `json:"exports"`
				Version string                     `json:"version"`
			}
			if err := json.Unmarshal(packageJSON, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.Version != plugin.Version {
				t.Fatalf("package version = %q, preinstall version = %q", manifest.Version, plugin.Version)
			}
			if plugin.HostOnly {
				return // the manifest is checked above; a host-only plugin ships no browser half
			}

			clientExport := manifest.Exports["./client"]
			var clientPath string
			if err := json.Unmarshal(clientExport, &clientPath); err != nil {
				var conditions map[string]string
				if err := json.Unmarshal(clientExport, &conditions); err != nil {
					t.Fatalf("decode ./client export: %v", err)
				}
				clientPath = conditions["default"]
			}
			if clientPath == "" {
				t.Fatal("package has no ./client export")
			}

			bundle, err := fs.ReadFile(pluginsFS, filepath.ToSlash(filepath.Join("plugins", plugin.Dir, strings.TrimPrefix(clientPath, "./"))))
			if err != nil {
				t.Fatal(err)
			}
			registration := regexp.MustCompile(`(?s)__ModuleLoader__\.load\s*\(\s*\{\s*id:\s*["']` + regexp.QuoteMeta(plugin.Name) + `["']`)
			if !registration.Match(bundle) {
				t.Fatalf("client bundle does not register package name %q via ModuleLoader.load", plugin.Name)
			}
		})
	}
}

func TestPreinstalledClientBundlesDoNotUseRemovedRuntimeModule(t *testing.T) {
	const removedModule = "@deepseek-ai/dsh-client-runtime"

	for _, plugin := range preinstalledPlugins {
		if plugin.HostOnly {
			continue // nothing to inspect: a host-only plugin has no client bundle
		}
		t.Run(plugin.Name, func(t *testing.T) {
			packageJSON, err := fs.ReadFile(pluginsFS, filepath.ToSlash(filepath.Join("plugins", plugin.Dir, "package.json")))
			if err != nil {
				t.Fatal(err)
			}

			var manifest struct {
				Exports map[string]json.RawMessage `json:"exports"`
				DSH     struct {
					Client struct {
						Inject []string `json:"inject"`
					} `json:"client"`
				} `json:"dsh"`
			}
			if err := json.Unmarshal(packageJSON, &manifest); err != nil {
				t.Fatal(err)
			}
			for _, dependency := range manifest.DSH.Client.Inject {
				if dependency == removedModule {
					t.Fatalf("manifest still injects removed module %q", removedModule)
				}
			}

			clientExport := manifest.Exports["./client"]
			var clientPath string
			if err := json.Unmarshal(clientExport, &clientPath); err != nil {
				var conditions map[string]string
				if err := json.Unmarshal(clientExport, &conditions); err != nil {
					t.Fatalf("decode ./client export: %v", err)
				}
				clientPath = conditions["default"]
			}
			bundle, err := fs.ReadFile(pluginsFS, filepath.ToSlash(filepath.Join("plugins", plugin.Dir, strings.TrimPrefix(clientPath, "./"))))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(bundle), removedModule) {
				t.Fatalf("client bundle still imports removed module %q", removedModule)
			}
		})
	}
}

func TestFileChangesUsesCurrentConversationService(t *testing.T) {
	bundle, err := fs.ReadFile(pluginsFS, "plugins/file-changes/lib/client.js")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(bundle)
	if strings.Contains(contents, `"conversationEvents"`) || strings.Contains(contents, "ctx.conversationEvents") {
		t.Fatal("file-changes still depends on the removed conversationEvents root service")
	}
	if !strings.Contains(contents, `"uiConversation"`) || !strings.Contains(contents, "ctx.uiConversation.events.register") {
		t.Fatal("file-changes does not register through the current uiConversation service")
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

// Every vendored plugin must carry the ownership marker, or preinstall cannot
// recognize its own copy once the desktop state directory is gone.
func TestPreinstalledPluginManifestsCarryOwnershipMarker(t *testing.T) {
	for _, plugin := range preinstalledPlugins {
		t.Run(plugin.Name, func(t *testing.T) {
			packageJSON, err := fs.ReadFile(pluginsFS, filepath.ToSlash(filepath.Join("plugins", plugin.Dir, "package.json")))
			if err != nil {
				t.Fatal(err)
			}
			var manifest struct {
				DSH struct {
					Desktop struct {
						Vendored bool `json:"vendored"`
					} `json:"desktop"`
				} `json:"dsh"`
			}
			if err := json.Unmarshal(packageJSON, &manifest); err != nil {
				t.Fatal(err)
			}
			if !manifest.DSH.Desktop.Vendored {
				t.Fatal("manifest is missing dsh.desktop.vendored; preinstall could not claim it after a state loss")
			}
		})
	}
}

// The regression this hardening exists for: with preinstall-state.json gone,
// an older copy of our own plugin must still be replaced instead of being
// mistaken for a user install and left to break the next DSH version.
func TestRunPreinstallUpgradesMarkerOwnedPluginAfterStateLoss(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)
	t.Setenv(stateDirEnv, t.TempDir())

	if _, err := runPreinstall(noopLogf); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(preinstallStatePath()); err != nil {
		t.Fatalf("drop state file: %v", err)
	}

	target := filepath.Join(dshHome, "profiles", "node_modules", "dsh-file-changes")
	stale := `{"name":"dsh-file-changes","version":"0.0.0","dsh":{"desktop":{"vendored":true}}}`
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := runPreinstall(noopLogf)
	if err != nil {
		t.Fatalf("runPreinstall: %v", err)
	}
	if !strings.Contains(status, "installed 1") {
		t.Fatalf("status = %q, want the marker-owned plugin upgraded", status)
	}

	var shipped string
	for _, plugin := range preinstalledPlugins {
		if plugin.Name == "dsh-file-changes" {
			shipped = plugin.Version
		}
	}
	restored, readable := installedPluginManifest(target)
	if !readable {
		t.Fatal("restored copy is unreadable")
	}
	if restored.Version != shipped {
		t.Fatalf("restored version = %q, want %q", restored.Version, shipped)
	}
	if !restored.DSH.Desktop.Vendored {
		t.Fatal("restored copy lost its ownership marker")
	}
}

// An untracked same-name directory is never overwritten, but it must say so
// loudly and name the path: a silent skip is what makes the failure look
// like a broken UI instead of a stale plugin.
func TestRunPreinstallWarnsAboutUntrackedPlugin(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)
	t.Setenv(stateDirEnv, t.TempDir())

	target := filepath.Join(dshHome, "profiles", "node_modules", "dsh-file-changes")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	userPkg := `{"name":"dsh-file-changes","version":"9.9.9"}`
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(userPkg), 0o644); err != nil {
		t.Fatal(err)
	}

	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	if _, err := runPreinstall(logf); err != nil {
		t.Fatalf("runPreinstall: %v", err)
	}

	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "dsh-file-changes") || !strings.Contains(joined, target) {
		t.Fatalf("expected a warning naming the plugin and its path, got:\n%s", joined)
	}
	raw, err := os.ReadFile(filepath.Join(target, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != userPkg {
		t.Fatalf("untracked install overwritten: %s", raw)
	}
}

// A markerless copy already at the version we ship needs no action, and a
// warning on every boot would only teach the reader to ignore the real one.
func TestRunPreinstallStaysQuietAboutMatchingUntrackedPlugin(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)
	t.Setenv(stateDirEnv, t.TempDir())

	shipped := ""
	for _, plugin := range preinstalledPlugins {
		if plugin.Name == "dsh-file-changes" {
			shipped = plugin.Version
		}
	}
	target := filepath.Join(dshHome, "profiles", "node_modules", "dsh-file-changes")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	userPkg := fmt.Sprintf(`{"name":"dsh-file-changes","version":%q}`, shipped)
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(userPkg), 0o644); err != nil {
		t.Fatal(err)
	}

	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	if _, err := runPreinstall(logf); err != nil {
		t.Fatalf("runPreinstall: %v", err)
	}
	for _, line := range logs {
		if strings.Contains(line, "dsh-file-changes") {
			t.Fatalf("expected no warning for a matching untracked copy, got: %s", line)
		}
	}
	raw, err := os.ReadFile(filepath.Join(target, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != userPkg {
		t.Fatalf("matching untracked copy overwritten: %s", raw)
	}
}

// A copy installed before the ownership marker existed must be re-copied once
// so a later state loss can still recognize it.
func TestRunPreinstallArmsOwnershipMarkerOnTrackedCopy(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)
	t.Setenv(stateDirEnv, t.TempDir())

	if _, err := runPreinstall(noopLogf); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dshHome, "profiles", "node_modules", "dsh-file-changes")
	manifest, readable := installedPluginManifest(target)
	if !readable || !manifest.DSH.Desktop.Vendored {
		t.Fatal("first install did not carry the ownership marker")
	}
	// Strip the marker to look like a copy from the previous desktop build.
	stripped := fmt.Sprintf(`{"name":%q,"version":%q}`, manifest.Name, manifest.Version)
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(stripped), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := runPreinstall(noopLogf)
	if err != nil {
		t.Fatalf("runPreinstall: %v", err)
	}
	if !strings.Contains(status, "installed 1") {
		t.Fatalf("status = %q, want the tracked copy re-copied to arm its marker", status)
	}
	armed, readable := installedPluginManifest(target)
	if !readable || !armed.DSH.Desktop.Vendored {
		t.Fatal("tracked copy was not armed with the ownership marker")
	}
}

// Uninstall must still remove a directory this app installed when only the
// manifest marker proves ownership.
func TestUninstallPreinstalledPluginUsesOwnershipMarker(t *testing.T) {
	dshHome := t.TempDir()
	t.Setenv("DSH_HOME", dshHome)
	t.Setenv(stateDirEnv, t.TempDir())

	if _, err := runPreinstall(noopLogf); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(preinstallStatePath()); err != nil {
		t.Fatalf("drop state file: %v", err)
	}

	if err := uninstallPreinstalledPlugin("file-changes", noopLogf); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	target := filepath.Join(dshHome, "profiles", "node_modules", "dsh-file-changes")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("marker-owned plugin directory survived uninstall")
	}
	raw, _ := os.ReadFile(filepath.Join(dshHome, "profiles", "web", "cordis.patch.yml"))
	if strings.Contains(string(raw), "id: file-changes") {
		t.Fatalf("patch block survived uninstall: %s", raw)
	}
}
