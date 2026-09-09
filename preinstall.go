package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed all:plugins
var pluginsFS embed.FS

// preinstallPlugin describes one plugin the desktop ships as preinstalled.
type preinstallPlugin struct {
	// ID is the Cordis insert id recorded in cordis.patch.yml.
	ID string
	// Name is the package name: it doubles as the node_modules directory and
	// the bare module specifier the loader resolves.
	Name string
	// Dir is the embedded directory under plugins/ that holds the plugin's
	// runtime files (package.json, lib/, client, cordis.patch.yml, ...).
	Dir string
	// Version is the vendored plugin version; a bump re-copies on next run so
	// a newer desktop build upgrades an already-installed plugin.
	Version string
	// Insert is the full cordis.patch.yml block to append (including the
	// leading "- insert:" line and trailing newline), or "" when the plugin
	// needs no extra config in the patch layer.
	Insert string
	// HostOnly marks a plugin with no browser half. The client-bundle checks
	// skip it; every other preinstalled plugin must still ship ./client.
	HostOnly bool
}

// preinstalledPlugins is the fixed set of plugins shipped by this build.
// Order matters: open-editor is listed before diff-review (its dependent), and
// each block is appended to cordis.patch.yml in this order.
var preinstalledPlugins = []preinstallPlugin{
	{
		ID:      "file-changes",
		Name:    "dsh-file-changes",
		Dir:     "file-changes",
		Version: "0.2.2",
		Insert: `- insert:
    - id: file-changes
      name: dsh-file-changes
`,
	},
	{
		ID:      "dsh-subagent-max",
		Name:    "@aaravarr/dsh-subagent-max",
		Dir:     "dsh-subagent-max",
		Version: "0.2.1",
		Insert: `- insert:
    - id: dsh-subagent-max
      name: '@aaravarr/dsh-subagent-max'
      config:
        subagentProvider: spawn
        toolName: subagent_with_model
        backgroundMode: continuable
        maxDepth: 3
`,
	},
	{
		ID:      "open-editor",
		Name:    "dsh-plugin-open-editor",
		Dir:     "open-editor",
		Version: "0.1.1",
		Insert: `- insert:
    - id: open-editor
      name: dsh-plugin-open-editor
`,
	},
	{
		ID:      "diff-review",
		Name:    "dsh-plugin-diff-review",
		Dir:     "diff-review",
		Version: "0.1.1",
		Insert: `- insert:
    - id: diff-review
      name: dsh-plugin-diff-review
`,
	},
	{
		ID:      "account-login",
		Name:    "dsh-account-login",
		Dir:     "dsh-account-login",
		Version: "0.1.3",
		Insert: `- insert:
    - id: account-login
      name: dsh-account-login
`,
	},
	{
		// Keeps sessions recorded before DSH renamed the "code" agent preset
		// resumable on 0.1.2+ by aliasing it to "ptc" in the user preset root.
		ID:       "agent-preset-compat",
		Name:     "dsh-agent-preset-compat",
		Dir:      "agent-preset-compat",
		Version:  "0.1.0",
		HostOnly: true,
		Insert: `- insert:
    - id: agent-preset-compat
      name: dsh-agent-preset-compat
`,
	},
}

const preinstallStateFile = "preinstall-state.json"

type installedPlugin struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type preinstallState struct {
	SchemaVersion int               `json:"schemaVersion"`
	Installed     []installedPlugin `json:"installed"` // plugins this desktop install created
}

// resolveDSHHome returns the DeepSeek Harness home directory, honoring DSH_HOME.
func resolveDSHHome() string {
	if h := strings.TrimSpace(os.Getenv("DSH_HOME")); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".dsh")
}

func preinstallStatePath() string {
	return filepath.Join(stateDir(), preinstallStateFile)
}

func loadPreinstallState(path string) preinstallState {
	raw, err := os.ReadFile(path)
	if err != nil {
		return preinstallState{SchemaVersion: 1}
	}
	var s preinstallState
	if json.Unmarshal(raw, &s) != nil {
		return preinstallState{SchemaVersion: 1}
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
	return s
}

func savePreinstallState(path string, s preinstallState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// pluginManifest is the slice of an installed plugin's package.json this app
// reads: its version and the ownership marker its own copies carry.
type pluginManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	DSH     struct {
		Desktop struct {
			Vendored bool `json:"vendored"`
		} `json:"desktop"`
	} `json:"dsh"`
}

// installedPluginManifest reads an installed plugin's manifest. The marker it
// carries lives in the plugin's own directory, so ownership survives losing the
// preinstall state directory — the failure that strands an incompatible copy
// after a DSH version bump.
func installedPluginManifest(target string) (pluginManifest, bool) {
	raw, err := os.ReadFile(filepath.Join(target, "package.json"))
	if err != nil {
		return pluginManifest{}, false
	}
	var manifest pluginManifest
	if json.Unmarshal(raw, &manifest) != nil {
		return pluginManifest{}, false
	}
	return manifest, true
}

// copyEmbeddedDir materializes an embedded plugins/ subdirectory to dst.
func copyEmbeddedDir(srcDir, dst string) error {
	fullSrc := filepath.ToSlash(filepath.Join("plugins", srcDir))
	prefix := fullSrc + "/"
	return fs.WalkDir(pluginsFS, fullSrc, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, prefix)
		if rel == "" || rel == "." {
			return nil
		}
		out := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := fs.ReadFile(pluginsFS, p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
}

// patchHasPlugin reports whether patch content already declares the given insert id.
func patchHasPlugin(content, id string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), "id: "+id) {
			return true
		}
	}
	return false
}

// appendPatch inserts the plugin block into cordis.patch.yml if not already
// present. It returns the path of a backup written before the first edit, or
// "" if no edit was needed.
func appendPatch(patchPath, id, block string, backupPath *string) (bool, error) {
	var content string
	if raw, err := os.ReadFile(patchPath); err == nil {
		content = string(raw)
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if patchHasPlugin(content, id) {
		return false, nil
	}
	if *backupPath == "" {
		if _, err := os.Stat(patchPath); err == nil {
			bak := patchPath + ".bak-dsh-desktop"
			if err := os.WriteFile(bak, []byte(content), 0o644); err != nil {
				return false, err
			}
			*backupPath = bak
		}
	}
	if err := os.MkdirAll(filepath.Dir(patchPath), 0o755); err != nil {
		return false, err
	}
	sep := ""
	if content != "" && !strings.HasSuffix(content, "\n") {
		sep = "\n"
	}
	// Keep one blank line between existing content and our block for readability.
	if content != "" && !strings.HasSuffix(content, "\n\n") {
		sep = "\n" + sep
	}
	out := content + sep + block
	return true, os.WriteFile(patchPath, []byte(out), 0o644)
}

// rollbackPreinstall undoes a partial run: removes directories it created and
// restores the patch file from the backup taken before the first edit.
func rollbackPreinstall(createdDirs []string, backupPath, patchPath string) {
	for _, d := range createdDirs {
		_ = os.RemoveAll(d)
	}
	if backupPath != "" {
		if raw, err := os.ReadFile(backupPath); err == nil {
			_ = os.WriteFile(patchPath, raw, 0o644)
		}
		_ = os.Remove(backupPath)
	}
}

// runPreinstall installs the shipped plugins into the DSH web profile,
// idempotently and without touching a plugin this app did not install (a copy
// with no ownership marker). It returns a short status line and an error on
// failure (after best-effort rollback of anything this run created).
func runPreinstall(logf func(format string, args ...any)) (string, error) {
	dshHome := resolveDSHHome()
	if dshHome == "" {
		return "", fmt.Errorf("preinstall: cannot resolve DSH home")
	}
	profileModules := filepath.Join(dshHome, "profiles", "node_modules")
	patchPath := filepath.Join(dshHome, "profiles", "web", "cordis.patch.yml")
	statePath := preinstallStatePath()

	state := loadPreinstallState(statePath)
	owned := make(map[string]string, len(state.Installed))
	for _, ip := range state.Installed {
		if ip.Name != "" {
			owned[ip.Name] = ip.Version
		}
	}

	var createdDirs []string
	var backupPath string
	changed := 0

	for _, p := range preinstalledPlugins {
		target := filepath.Join(profileModules, filepath.FromSlash(p.Name))

		if _, err := os.Lstat(target); err == nil {
			version, isOurs := owned[p.Name]
			// The manifest marker is the second ownership source: a lost or
			// stale preinstall-state.json must not strand an incompatible copy
			// of our own plugin after a DSH version bump. The name has to match
			// too, so a marker copied onto some other package claims nothing.
			installed, readable := installedPluginManifest(target)
			marked := readable && installed.Name == p.Name && installed.DSH.Desktop.Vendored
			if marked {
				version, isOurs = installed.Version, true
			}

			switch {
			case isOurs && version == p.Version && marked:
				continue // ours, current, and already self-identifying
			case isOurs && version != p.Version:
				// Version bump: replace our previous copy.
				if err := os.RemoveAll(target); err != nil {
					rollbackPreinstall(createdDirs, backupPath, patchPath)
					return "", fmt.Errorf("preinstall %s: remove old: %w", p.Name, err)
				}
			case !isOurs:
				// A copy we did not install is never touched. Warn whenever it
				// could be an outdated or unreadable version of ours — that is
				// the state that leaves an incompatible plugin in place and
				// breaks the whole UI; a matching version we did not write is
				// harmless, and warning every boot would only be noise.
				if !readable || installed.Version == "" || installed.Version != p.Version {
					logf("preinstall: %s %s is present but was not installed by this app (shipped %s); leaving it untouched. Delete %s if that plugin fails to load", p.Name, installed.Version, p.Version, target)
				}
				continue
			default:
				// Ours at the shipped version but copied before the marker
				// existed: re-copy below so a later state loss still knows.
			}
		}

		if err := copyEmbeddedDir(p.Dir, target); err != nil {
			rollbackPreinstall(createdDirs, backupPath, patchPath)
			return "", fmt.Errorf("preinstall %s: copy: %w", p.Name, err)
		}
		createdDirs = append(createdDirs, target)
		owned[p.Name] = p.Version
		changed++
		logf("preinstall: installed %s", p.Name)

		if p.Insert != "" {
			appended, err := appendPatch(patchPath, p.ID, p.Insert, &backupPath)
			if err != nil {
				rollbackPreinstall(createdDirs, backupPath, patchPath)
				return "", fmt.Errorf("preinstall %s: patch: %w", p.Name, err)
			}
			if appended {
				logf("preinstall: registered %s in cordis.patch.yml", p.Name)
			}
		}
	}

	if changed > 0 {
		entries := make([]installedPlugin, 0, len(owned))
		for name, version := range owned {
			entries = append(entries, installedPlugin{Name: name, Version: version})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
		if err := savePreinstallState(statePath, preinstallState{SchemaVersion: 1, Installed: entries}); err != nil {
			rollbackPreinstall(createdDirs, backupPath, patchPath)
			return "", fmt.Errorf("preinstall: save state: %w", err)
		}
	}

	if changed == 0 {
		return "preinstall: up to date", nil
	}
	return fmt.Sprintf("preinstall: installed %d plugin(s)", changed), nil
}

// removePatchBlock deletes one insert block (the exact string we appended)
// from cordis.patch.yml, with a backup written before the edit.
func removePatchBlock(patchPath, block string) error {
	raw, err := os.ReadFile(patchPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	content := string(raw)
	idx := strings.Index(content, block)
	if idx < 0 {
		return nil
	}
	bak := patchPath + ".bak-dsh-desktop"
	if err := os.WriteFile(bak, raw, 0o644); err != nil {
		return err
	}
	// Drop the block plus any blank separator lines immediately before it.
	start := idx
	for start > 0 && content[start-1] == '\n' {
		start--
	}
	out := content[:start] + content[idx+len(block):]
	out = strings.TrimLeft(out, "\n")
	if err := os.WriteFile(patchPath, []byte(out), 0o644); err != nil {
		return err
	}
	_ = os.Remove(bak)
	return nil
}

// uninstallPreinstalledPlugin removes one preinstalled plugin: its node_modules
// directory (only when this app installed it, judged by the state record or the
// ownership marker) and its cordis.patch.yml insert block. It is the
// single-plugin inverse of runPreinstall.
func uninstallPreinstalledPlugin(id string, logf func(format string, args ...any)) error {
	var p *preinstallPlugin
	for i := range preinstalledPlugins {
		if preinstalledPlugins[i].ID == id {
			p = &preinstalledPlugins[i]
			break
		}
	}
	if p == nil {
		return fmt.Errorf("unknown preinstalled plugin %q", id)
	}

	dshHome := resolveDSHHome()
	if dshHome == "" {
		return fmt.Errorf("cannot resolve DSH home")
	}
	profileModules := filepath.Join(dshHome, "profiles", "node_modules")
	patchPath := filepath.Join(dshHome, "profiles", "web", "cordis.patch.yml")
	statePath := preinstallStatePath()

	state := loadPreinstallState(statePath)
	owned := false
	for _, ip := range state.Installed {
		if ip.Name == p.Name {
			owned = true
			break
		}
	}
	target := filepath.Join(profileModules, filepath.FromSlash(p.Name))
	if !owned {
		manifest, readable := installedPluginManifest(target)
		owned = readable && manifest.DSH.Desktop.Vendored
	}

	// Only remove the directory when we created it (state record or marker).
	if owned {
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove %s: %w", p.Name, err)
		}
		logf("uninstall: removed %s", p.Name)
	}

	if p.Insert != "" {
		if err := removePatchBlock(patchPath, p.Insert); err != nil {
			return fmt.Errorf("unpatch %s: %w", p.Name, err)
		}
		logf("uninstall: removed %s from cordis.patch.yml", p.Name)
	}

	entries := make([]installedPlugin, 0, len(state.Installed))
	for _, ip := range state.Installed {
		if ip.Name != p.Name {
			entries = append(entries, ip)
		}
	}
	if err := savePreinstallState(statePath, preinstallState{SchemaVersion: 1, Installed: entries}); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}
