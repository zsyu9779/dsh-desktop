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
	// Insert is the full cordis.patch.yml block to append (including the
	// leading "- insert:" line and trailing newline), or "" when the plugin
	// needs no extra config in the patch layer.
	Insert string
}

// preinstalledPlugins is the fixed set of plugins shipped by this build.
// Order matters: open-editor is listed before diff-review (its dependent), and
// each block is appended to cordis.patch.yml in this order.
var preinstalledPlugins = []preinstallPlugin{
	{
		ID:   "file-changes",
		Name: "dsh-file-changes",
		Dir:  "file-changes",
		Insert: `- insert:
    - id: file-changes
      name: dsh-file-changes
`,
	},
	{
		ID:   "dsh-subagent-max",
		Name: "@aaravarr/dsh-subagent-max",
		Dir:  "dsh-subagent-max",
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
		ID:   "open-editor",
		Name: "dsh-plugin-open-editor",
		Dir:  "open-editor",
		Insert: `- insert:
    - id: open-editor
      name: dsh-plugin-open-editor
`,
	},
	{
		ID:   "diff-review",
		Name: "dsh-plugin-diff-review",
		Dir:  "diff-review",
		Insert: `- insert:
    - id: diff-review
      name: dsh-plugin-diff-review
`,
	},
}

const preinstallStateFile = "preinstall-state.json"

type preinstallState struct {
	SchemaVersion int      `json:"schemaVersion"`
	Installed     []string `json:"installed"` // plugin Names this desktop install created
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
// idempotently and without touching a plugin the user already installed. It
// returns a short status line and an error on failure (after best-effort
// rollback of anything this run created).
func runPreinstall(logf func(format string, args ...any)) (string, error) {
	dshHome := resolveDSHHome()
	if dshHome == "" {
		return "", fmt.Errorf("preinstall: cannot resolve DSH home")
	}
	profileModules := filepath.Join(dshHome, "profiles", "node_modules")
	patchPath := filepath.Join(dshHome, "profiles", "web", "cordis.patch.yml")
	statePath := preinstallStatePath()

	state := loadPreinstallState(statePath)
	owned := make(map[string]bool, len(state.Installed))
	for _, n := range state.Installed {
		owned[n] = true
	}

	var createdDirs []string
	var backupPath string
	newlyInstalled := 0

	for _, p := range preinstalledPlugins {
		target := filepath.Join(profileModules, filepath.FromSlash(p.Name))

		if _, err := os.Lstat(target); err == nil {
			if owned[p.Name] {
				continue // already ours from a previous run
			}
			logf("preinstall: %s already present, leaving user install untouched", p.Name)
			continue
		}

		if err := copyEmbeddedDir(p.Dir, target); err != nil {
			rollbackPreinstall(createdDirs, backupPath, patchPath)
			return "", fmt.Errorf("preinstall %s: copy: %w", p.Name, err)
		}
		createdDirs = append(createdDirs, target)
		owned[p.Name] = true
		newlyInstalled++
		logf("preinstall: installed %s", p.Name)

		if p.Insert != "" {
			changed, err := appendPatch(patchPath, p.ID, p.Insert, &backupPath)
			if err != nil {
				rollbackPreinstall(createdDirs, backupPath, patchPath)
				return "", fmt.Errorf("preinstall %s: patch: %w", p.Name, err)
			}
			if changed {
				logf("preinstall: registered %s in cordis.patch.yml", p.Name)
			}
		}
	}

	if newlyInstalled > 0 || len(state.Installed) != len(owned) {
		names := make([]string, 0, len(owned))
		for n := range owned {
			names = append(names, n)
		}
		// Stable order for reproducible state files.
		sort.Strings(names)
		if err := savePreinstallState(statePath, preinstallState{SchemaVersion: 1, Installed: names}); err != nil {
			rollbackPreinstall(createdDirs, backupPath, patchPath)
			return "", fmt.Errorf("preinstall: save state: %w", err)
		}
	}

	if newlyInstalled == 0 {
		return "preinstall: up to date", nil
	}
	return fmt.Sprintf("preinstall: installed %d plugin(s)", newlyInstalled), nil
}
