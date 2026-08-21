package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// dshVersionPattern matches npm-style versions: 1.2.3, 0.1.1-rc.2, etc.
var dshVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// desktopConfig is the runtime-writable desktop configuration. dshVersion
// overrides the compiled-in dshPackage pin without rebuilding the binary, so a
// new DeepSeek Harness release can be adopted by the running app.
type desktopConfig struct {
	DSHVersion string `json:"dshVersion"`
}

var (
	configMu     sync.Mutex
	configPath   = filepath.Join(stateDir(), "config.json")
	registryHost = "https://registry.npmjs.org"
)

// loadDesktopConfig reads the runtime config; a missing or malformed file
// yields the zero value (no version override).
func loadDesktopConfig() desktopConfig {
	configMu.Lock()
	defer configMu.Unlock()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return desktopConfig{}
	}
	var c desktopConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return desktopConfig{}
	}
	return c
}

// saveDesktopConfig atomically writes the runtime config.
func saveDesktopConfig(c desktopConfig) error {
	configMu.Lock()
	defer configMu.Unlock()
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, configPath)
}

// pinnedDSHPackage returns the effective DSH package spec: the runtime
// override from config.json when set, otherwise the compiled-in default pin.
func pinnedDSHPackage() string {
	if v := strings.TrimSpace(loadDesktopConfig().DSHVersion); v != "" {
		return "@deepseek-ai/dsh@" + v
	}
	return dshPackage
}

// currentDSHVersion returns the bare version currently pinned (without the
// package prefix), for display and comparison.
func currentDSHVersion() string {
	return strings.TrimPrefix(pinnedDSHPackage(), "@deepseek-ai/dsh@")
}

// dshUpdateInfo is the payload pushed to the frontend about available updates.
type dshUpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"hasUpdate"`
	Error     string `json:"error,omitempty"`
}

// fetchLatestDSHVersion queries the npm registry for the latest dist-tag of
// the DSH package. A short timeout keeps startup checks non-blocking.
func fetchLatestDSHVersion() (string, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(registryHost + "/@deepseek-ai/dsh")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm registry returned %s", resp.Status)
	}
	var reg struct {
		DistTags map[string]string `json:"dist-tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return "", err
	}
	latest := reg.DistTags["latest"]
	if latest == "" {
		return "", fmt.Errorf("npm registry has no latest dist-tag")
	}
	return latest, nil
}

// CheckDSHUpdate returns the current pinned version, the latest published
// version, and whether an update is available. Registry errors (offline, etc.)
// are reported in the payload instead of as Go errors so the UI can show a
// "check failed" state without a dialog.
func (a *App) CheckDSHUpdate() dshUpdateInfo {
	current := currentDSHVersion()
	latest, err := fetchLatestDSHVersion()
	if err != nil {
		return dshUpdateInfo{Current: current, Error: err.Error()}
	}
	return dshUpdateInfo{
		Current:   current,
		Latest:    latest,
		HasUpdate: latest != current,
	}
}

// DSHVersion returns the bare version currently pinned, for display.
func (a *App) DSHVersion() string {
	return currentDSHVersion()
}

// SetDSHVersion switches the pinned DSH version at runtime and restarts the
// harness. If the new version fails to become ready, the previous version is
// restored and the harness restarted with it. The switch runs in the
// background so the UI is not blocked by the readiness wait.
func (a *App) SetDSHVersion(version string) error {
	version = strings.TrimSpace(version)
	if !dshVersionPattern.MatchString(version) {
		return fmt.Errorf("无效的版本号 %q", version)
	}
	old := currentDSHVersion()
	if version == old {
		return nil
	}
	if err := saveDesktopConfig(desktopConfig{DSHVersion: version}); err != nil {
		return err
	}
	a.dsh.logf("切换 DSH 版本: %s -> %s", old, version)
	a.dsh.restart()
	go a.watchVersionSwitch(old, version)
	return nil
}

// watchVersionSwitch waits for the harness to reach ready after a version
// switch; on failure or timeout it rolls back to the previous version and
// restarts the harness with it.
func (a *App) watchVersionSwitch(old, version string) {
	// restart() returns before the new process sets "starting", so first wait
	// for that transition to avoid mistaking the stale ready state for success.
	deadline := time.Now().Add(readyTimeout)
	sawStarting := false
	for time.Now().Before(deadline) {
		state := a.dsh.current().State
		if state == "starting" {
			sawStarting = true
		}
		if sawStarting && state == "ready" {
			a.dsh.logf("DSH %s 就绪", version)
			runtime.EventsEmit(a.ctx, "dsh-update", a.CheckDSHUpdate())
			return
		}
		if sawStarting && (state == "error" || state == "exited") {
			break // new version failed to start
		}
		time.Sleep(300 * time.Millisecond)
	}
	a.dsh.logf("DSH %s 未能就绪，回滚到 %s", version, old)
	_ = saveDesktopConfig(desktopConfig{DSHVersion: old})
	a.dsh.restart()
}
