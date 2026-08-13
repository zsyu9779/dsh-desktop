package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the root Wails application. It owns the DeepSeek Harness process.
type App struct {
	ctx context.Context
	dsh *dshManager
}

// NewApp creates a new App instance.
func NewApp() *App {
	a := &App{}
	a.dsh = newDSHManager(a)
	return a
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.dsh.start()
}

// shutdown is called when the app is about to exit.
func (a *App) shutdown(ctx context.Context) {
	a.dsh.stop()
}

// beforeClose lets us always allow the window to close; cleanup runs in shutdown.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
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

// Quit exits the application.
func (a *App) Quit() {
	runtime.Quit(a.ctx)
}
