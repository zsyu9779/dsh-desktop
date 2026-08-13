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
	"syscall"
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
	running := m.cmd != nil
	m.mu.Unlock()
	if running {
		return
	}
	go m.run()
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
	cmd := m.cmd
	done := m.done
	m.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	// Kill the whole process group (npx -> node -> dsh) rather than just npx.
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}

	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(killGrace):
		if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = cmd.Process.Kill()
		}
		<-done
	}
}

// run performs the actual launch: pick a port, spawn dsh, wait for readiness.
func (m *dshManager) run() {
	m.stopping.Store(false)
	m.setStatus("starting", "正在启动 DeepSeek Harness…")

	port, err := findPort()
	if err != nil {
		m.fail(fmt.Errorf("无法分配端口: %w", err))
		return
	}

	cmd, err := m.buildCommand(port)
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

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
func (m *dshManager) buildCommand(port int) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	portStr := fmt.Sprintf("%d", port)

	if custom := strings.TrimSpace(os.Getenv("DSH_COMMAND")); custom != "" {
		parts := strings.Fields(custom)
		args := append(parts[1:], "web", "--host", "127.0.0.1", "--port", portStr, "--trusted-host", "127.0.0.1")
		cmd = exec.Command(parts[0], args...)
	} else {
		cmd = exec.Command("npx", "-y", dshPackage, "web", "--host", "127.0.0.1", "--port", portStr, "--trusted-host", "127.0.0.1")
	}

	// Working directory: the user's project, overridable via DSH_WORKSPACE.
	wd := strings.TrimSpace(os.Getenv("DSH_WORKSPACE"))
	if wd == "" {
		if home, err := os.UserHomeDir(); err == nil {
			wd = home
		}
	}
	if wd != "" {
		cmd.Dir = wd
	}

	cmd.Env = os.Environ()
	return cmd, nil
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
