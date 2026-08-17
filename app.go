package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the root Wails application. It owns the DeepSeek Harness process.
type App struct {
	ctx    context.Context
	dsh    *dshManager
	remote *remoteManager
}

// NewApp creates a new App instance.
func NewApp() *App {
	a := &App{}
	a.dsh = newDSHManager(a)
	a.remote = newRemoteManager(a)
	return a
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	enableNativeFullscreen()
	// Pre-install the shipped plugins before the harness boots so it picks
	// them up on first load. Failure is non-fatal: the shell still starts and
	// only loses the preinstalled plugins, so log and continue.
	if status, err := runPreinstall(a.dsh.logf); err != nil {
		a.dsh.logf("preinstall: %v", err)
	} else {
		a.dsh.logf("%s", status)
	}
	a.dsh.start()
}

// shutdown is called when the app is about to exit.
func (a *App) shutdown(ctx context.Context) {
	a.remote.disable()
	a.dsh.stop()
}

// beforeClose stops the managed service before allowing the window to close.
// On macOS, closing the last window does not necessarily terminate the app, so
// relying on OnShutdown alone would leave npm/pnpm/node/dsh running in the background.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	a.dsh.stop()
	return false
}

// Status returns the current DeepSeek Harness status.
func (a *App) Status() status {
	return a.dsh.current()
}

// Retry stops and restarts DeepSeek Harness.
func (a *App) Retry() {
	a.dsh.restart()
}

// OpenInBrowser opens the DeepSeek Harness UI in the system browser.
func (a *App) OpenInBrowser() {
	if url := a.dsh.current().URL; url != "" {
		runtime.BrowserOpenURL(a.ctx, url)
	}
}

// OpenNodeJS opens the Node.js download page in the system browser.
func (a *App) OpenNodeJS() {
	runtime.BrowserOpenURL(a.ctx, "https://nodejs.org")
}

// Logs returns recent DeepSeek Harness log lines.
func (a *App) Logs() string {
	return a.dsh.logsString()
}

// EnableRemote starts the authenticated LAN proxy for phone remote control.
func (a *App) EnableRemote() (remoteStatus, error) {
	s, err := a.remote.enable(a.dsh.current().URL)
	if err != nil {
		return s, err
	}
	a.emitRemote(s)
	return s, nil
}

// DisableRemote stops the remote proxy and clears the pending pairing code.
func (a *App) DisableRemote() {
	a.remote.disable()
	a.emitRemote(a.remote.status())
}

// RemoteStatus returns the current remote control state.
func (a *App) RemoteStatus() remoteStatus {
	return a.remote.status()
}

// RegenerateRemoteToken rotates only the pending one-time pairing code.
// Existing paired Devices remain authorized until explicitly revoked.
func (a *App) RegenerateRemoteToken() remoteStatus {
	s := a.remote.regenerateToken()
	a.emitRemote(s)
	return s
}

// ListDevices returns the paired devices.
func (a *App) ListDevices() []deviceIdentity {
	return a.remote.listDevices()
}

// RevokeDevice revokes a paired device by ID, returning whether it existed.
func (a *App) RevokeDevice(deviceID string) bool {
	return a.remote.revokeDevice(deviceID)
}

// SetAllowPrivileged toggles whether remote Devices may call sensitive methods
// (settings / credentials / agentPreset, etc.).
func (a *App) SetAllowPrivileged(enabled bool) {
	a.remote.setAllowPrivileged(enabled)
	a.emitRemote(a.remote.status())
}

// emitRemote pushes a remote status snapshot to the frontend.
func (a *App) emitRemote(s remoteStatus) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "remote", s)
	}
}

// Quit exits the application.
func (a *App) Quit() {
	runtime.Quit(a.ctx)
}
