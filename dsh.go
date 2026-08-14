package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	// dshPackage is the pinned upstream DeepSeek Harness release. Bump this to
	// track a newer release, or set DSH_COMMAND to override the launcher entirely.
	dshPackage = "@deepseek-ai/dsh@0.1.0-rc.6"

	// preferredPort keeps the origin stable across launches so the web UI's
	// localStorage (settings, etc.) persists. We fall back to a random free port
	// if it is already taken.
	preferredPort = 3080

	// readyTimeout bounds how long we wait for the web UI to come up (the first
	// run also pays the npx download + first-build cost).
	readyTimeout = 180 * time.Second

	// killGrace is how long we wait for the process tree to exit after SIGTERM
	// before escalating to SIGKILL.
	killGrace = 3 * time.Second

	// logMaxLines caps the in-memory ring of recent log lines surfaced to the UI.
	logMaxLines = 500
)

// status is the JSON payload pushed to the frontend and returned by Status().
type status struct {
	State   string `json:"state"`   // starting | ready | error | exited
	URL     string `json:"url"`     // set once the web UI is up
	Port    int    `json:"port"`    // set once the port is chosen
	Message string `json:"message"` // human-readable status line
}

// dshManager owns the lifecycle of the DeepSeek Harness child process.
type dshManager struct {
	app *App

	stopping atomic.Bool

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	runDone chan struct{}
	cmd     *exec.Cmd
	done    chan struct{} // closed exactly once when the process exits
	exitErr error         // set before done is closed
	url     string
	port    int
	state   string
	message string

	logsMu sync.Mutex
	logs   []string
}

func newDSHManager(app *App) *dshManager {
	return &dshManager{
		app:     app,
		state:   "starting",
		message: "正在准备启动…",
	}
}

// current returns a snapshot of the manager status.
func (m *dshManager) current() status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return status{State: m.state, URL: m.url, Port: m.port, Message: m.message}
}

// setStatus records the state and pushes it to the frontend via an event.
func (m *dshManager) setStatus(state, message string) {
	m.mu.Lock()
	m.state = state
	m.message = message
	url := m.url
	port := m.port
	m.mu.Unlock()
	if m.app.ctx != nil {
		runtime.EventsEmit(m.app.ctx, "status", status{State: state, URL: url, Port: port, Message: message})
	}
}

// start launches the harness in the background if it is not already running.
func (m *dshManager) start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.stopping.Store(false)
	m.running = true
	m.cancel = cancel
	m.runDone = done
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			if m.runDone == done {
				m.running = false
				m.cancel = nil
				m.runDone = nil
			}
			m.mu.Unlock()
			close(done)
		}()
		m.run(ctx)
	}()
}

// restart stops any running instance and starts a fresh one.
func (m *dshManager) restart() {
	m.stop()
	m.stopping.Store(false)
	m.start()
}

// stop terminates the harness process tree and waits for it to exit.
func (m *dshManager) stop() {
	m.stopping.Store(true)

	m.mu.Lock()
	cancel := m.cancel
	cmd := m.cmd
	done := m.done
	runDone := m.runDone
	m.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		// Terminate the whole process tree (npx -> node -> dsh), not just npx.
		// Signal the group before cancelling CommandContext, whose default kill
		// targets only the immediate npx process.
		terminateProcessTree(cmd, false)
	}
	if cancel != nil {
		cancel()
	}

	if cmd != nil && cmd.Process != nil {
		if done != nil {
			select {
			case <-done:
			case <-time.After(killGrace):
				terminateProcessTree(cmd, true)
				<-done
			}
		}
	}

	// Wait for startup work to observe cancellation too. This closes the race
	// where the window is closed just before the command handle is published.
	if runDone != nil {
		select {
		case <-runDone:
		case <-time.After(killGrace):
		}
	}
}

// run performs the actual launch: check env, pick a port, spawn dsh, and wait
// for readiness. Its context is cancelled when the window or app closes.
func (m *dshManager) run(ctx context.Context) {
	// 1) Environment check: Node.js / npx must be present.
	m.setStatus("starting", "正在检查环境…")
	if err := m.checkEnvironment(); err != nil {
		if ctx.Err() != nil {
			return
		}
		m.fail(err)
		return
	}

	// 2) Launch + readiness. npx downloads the pinned package on first run.
	m.setStatus("starting", "正在启动 DeepSeek Harness…（首次运行需下载依赖，请稍候）")

	port, err := findPort()
	if err != nil {
		m.fail(fmt.Errorf("无法分配端口: %w", err))
		return
	}

	cmd, err := m.buildCommand(ctx, port)
	if err != nil {
		m.fail(fmt.Errorf("无法创建进程: %w", err))
		return
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.fail(fmt.Errorf("无法捕获输出: %w", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.fail(fmt.Errorf("无法捕获错误输出: %w", err))
		return
	}

	configureSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return
		}
		m.fail(fmt.Errorf("启动 dsh 失败: %w", err))
		return
	}

	m.mu.Lock()
	m.cmd = cmd
	m.port = port
	m.url = fmt.Sprintf("http://127.0.0.1:%d", port)
	m.done = make(chan struct{})
	done := m.done
	m.mu.Unlock()

	m.logf("dsh 已启动 (pid=%d, port=%d)", cmd.Process.Pid, port)

	go m.pump(stdout)
	go m.pump(stderr)

	// Single owner of cmd.Wait(); closes done once with the exit error.
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		m.exitErr = err
		m.mu.Unlock()
		close(done)
	}()

	readyCh := m.waitReady(ctx, port)

	select {
	case ready := <-readyCh:
		if !ready {
			m.fail(fmt.Errorf("等待 DeepSeek Harness 就绪超时 (%v)", readyTimeout))
			return
		}
	case <-done:
		m.markStopped()
		if !m.stopping.Load() {
			m.fail(fmt.Errorf("DeepSeek Harness 启动过程中退出: %v", m.exitError()))
		}
		return
	case <-ctx.Done():
		<-done
		m.markStopped()
		return
	}

	m.setStatus("ready", "DeepSeek Harness 已就绪")

	// Block until the process exits after we are ready.
	<-done
	m.markStopped()

	if m.stopping.Load() {
		return
	}

	err = m.exitError()
	m.logf("DeepSeek Harness 进程已退出: %v", err)
	m.setStatus("exited", fmt.Sprintf("DeepSeek Harness 已退出: %v", err))

	// Once the frontend has redirected to the DSH UI it can no longer receive
	// events, so surface the exit natively and quit.
	if m.app.ctx != nil {
		_, _ = runtime.MessageDialog(m.app.ctx, runtime.MessageDialogOptions{
			Type:    runtime.ErrorDialog,
			Title:   "DeepSeek Harness 已退出",
			Message: fmt.Sprintf("服务进程意外退出：%v", err),
		})
		runtime.Quit(m.app.ctx)
	}
}

// exitError returns the recorded exit error (callers hold no lock).
func (m *dshManager) exitError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exitErr
}

// markStopped clears the process handle now that it has exited.
func (m *dshManager) markStopped() {
	m.mu.Lock()
	m.cmd = nil
	m.done = nil
	m.mu.Unlock()
}

// fail records a terminal startup error and surfaces it to the splash screen.
func (m *dshManager) fail(err error) {
	m.logf("启动失败: %v", err)
	m.markStopped()
	m.setStatus("error", err.Error())
}

// waitReady polls the local web UI until it responds or the context is cancelled.
func (m *dshManager) waitReady(ctx context.Context, port int) <-chan bool {
	ch := make(chan bool, 1)
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		url := fmt.Sprintf("http://127.0.0.1:%d/", port)
		start := time.Now()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				ch <- false
				return
			case <-ticker.C:
				if time.Since(start) > readyTimeout {
					ch <- false
					return
				}
				resp, err := client.Get(url)
				if err == nil {
					_ = resp.Body.Close()
					ch <- true
					return
				}
				elapsed := int(time.Since(start).Seconds())
				m.setStatus("starting", fmt.Sprintf("正在启动 DeepSeek Harness…（%d 秒，首次运行需下载依赖）", elapsed))
			}
		}
	}()
	return ch
}

// pump streams a child output pipe into the log ring and log file.
func (m *dshManager) pump(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		m.logf("%s", scanner.Text())
	}
}

// buildCommand constructs the dsh launcher command.
func (m *dshManager) buildCommand(ctx context.Context, port int) (*exec.Cmd, error) {
	portStr := fmt.Sprintf("%d", port)
	webArgs := []string{"web", "--host", "127.0.0.1", "--port", portStr, "--trusted-host", "127.0.0.1"}

	var bin string
	var args []string
	if custom := strings.TrimSpace(os.Getenv("DSH_COMMAND")); custom != "" {
		parts := strings.Fields(custom)
		bin = parts[0]
		args = append(parts[1:], webArgs...)
	} else {
		bin = "npx"
		args = append([]string{"-y", dshPackage}, webArgs...)
	}

	// Resolve the launcher's absolute path explicitly: macOS GUI apps are
	// launched by launchd with a minimal PATH that omits Homebrew/nvm/fnm/volta,
	// so `npx` would otherwise not be found even when Node.js is installed.
	binPath, err := findExecutable(bin)
	if err != nil {
		return nil, fmt.Errorf("未找到 %q，请先安装 Node.js（需要 npx）: %w", bin, err)
	}

	cmd := exec.CommandContext(ctx, binPath, args...)

	// Working directory: the user's project, overridable via DSH_WORKSPACE.
	if wd := workspaceDir(); wd != "" {
		cmd.Dir = wd
	}

	// Extend PATH so npx's `#!/usr/bin/env node` shebang and any subprocesses
	// (pnpm, node) resolve from the same Node.js installation.
	cmd.Env = augmentedEnv()
	if bin == "npx" {
		cmd.Env = withEnv(cmd.Env, "NPM_CONFIG_CACHE", managedNPMCacheDir())
	}
	return cmd, nil
}

// findExecutable returns the absolute path to `name`. It first uses the
// standard lookup (which on Windows also honours PATHEXT), then falls back to
// searching common Node.js install locations for macOS GUI launches.
func findExecutable(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	if filepath.IsAbs(name) {
		if isExec(name) {
			return name, nil
		}
		return "", fmt.Errorf("%s: executable file not found", name)
	}
	for _, dir := range nodeBinPaths() {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if isExec(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s: executable file not found", name)
}

// isExec reports whether p is an existing executable file.
func isExec(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// nodeBinPaths returns candidate directories where Node.js tooling is commonly
// installed, in precedence order, followed by the standard system dirs and the
// current process PATH.
func nodeBinPaths() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		nvmBase := filepath.Join(home, ".nvm", "versions", "node")
		if entries, err := os.ReadDir(nvmBase); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					dirs = append(dirs, filepath.Join(nvmBase, e.Name(), "bin"))
				}
			}
		}
		dirs = append(dirs,
			filepath.Join(home, ".volta", "bin"),
			filepath.Join(home, ".local", "share", "fnm"),
			filepath.Join(home, ".fnm"),
			filepath.Join(home, ".asdf", "shims"),
		)
	}
	dirs = append(dirs,
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/opt/local/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	)
	dirs = append(dirs, filepath.SplitList(os.Getenv("PATH"))...)
	return dirs
}

// augmentedEnv returns the process environment with PATH extended to include
// nodeBinPaths, so the child's `#!/usr/bin/env node` shebangs resolve.
func augmentedEnv() []string {
	aug := strings.Join(nodeBinPaths(), string(os.PathListSeparator))
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	set := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			out = append(out, "PATH="+aug)
			set = true
		} else {
			out = append(out, kv)
		}
	}
	if !set {
		out = append(out, "PATH="+aug)
	}
	return out
}

// withEnv returns env with key replaced by value.
func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			out = append(out, prefix+value)
			replaced = true
		} else {
			out = append(out, kv)
		}
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}

// managedNPMCacheDir isolates dsh-desktop from concurrent npm/npx clients and
// stale shared _npx state, which can otherwise produce
// "sh: dsh: command not found" even though the package is installed.
func managedNPMCacheDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".dsh-desktop", "npm-cache-v1")
	}
	return filepath.Join(os.TempDir(), "dsh-desktop-npm-cache-v1")
}

// workspaceDir returns the directory dsh should run in (DSH_WORKSPACE or home).
func workspaceDir() string {
	if wd := strings.TrimSpace(os.Getenv("DSH_WORKSPACE")); wd != "" {
		return wd
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

// checkEnvironment verifies Node.js / npx are available and logs their paths.
func (m *dshManager) checkEnvironment() error {
	npxPath, err := findExecutable("npx")
	if err != nil {
		return fmt.Errorf("未检测到 Node.js / npx。请先安装 Node.js（https://nodejs.org）后重新打开本应用。")
	}
	m.logf("npx: %s", npxPath)
	if nodePath, err := findExecutable("node"); err == nil {
		if out, err := exec.Command(nodePath, "--version").Output(); err == nil {
			m.logf("node: %s (%s)", nodePath, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// findPort returns the preferred port if free, otherwise a random free port.
func findPort() (int, error) {
	if port, err := tryBind(preferredPort); err == nil {
		return port, nil
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// tryBind reports whether the given localhost port is free and returns it.
func tryBind(port int) (int, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return 0, err
	}
	_ = l.Close()
	return port, nil
}

// logf appends a line to the in-memory ring and the on-disk log file.
func (m *dshManager) logf(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	m.logsMu.Lock()
	m.logs = append(m.logs, line)
	if len(m.logs) > logMaxLines {
		m.logs = m.logs[len(m.logs)-logMaxLines:]
	}
	m.logsMu.Unlock()
	_ = m.appendLogFile(line)
}

// logsString returns the recent log lines joined as text.
func (m *dshManager) logsString() string {
	m.logsMu.Lock()
	defer m.logsMu.Unlock()
	return strings.Join(m.logs, "\n")
}

// appendLogFile writes a line to ~/.dsh-desktop/logs/dsh.log (best effort).
func (m *dshManager) appendLogFile(line string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	logDir := filepath.Join(home, ".dsh-desktop", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(logDir, "dsh.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(time.Now().Format("2006-01-02 15:04:05") + " " + line + "\n")
	return err
}
