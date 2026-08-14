import './style.css';

// Wails v2 injects the runtime and bound Go methods as window globals.
const runtime = window.runtime;
const App = window.go.main.App;

const statusText = document.getElementById('status-text');
const spinner = document.getElementById('spinner');
const progress = document.getElementById('progress');
const actions = document.getElementById('actions');
const logsEl = document.getElementById('logs');
const splash = document.getElementById('splash');
const harnessFrame = document.getElementById('harness-frame');

let harnessURL = '';
let enteringHarness = false;

function setBusy(busy) {
    spinner.classList.toggle('hidden', !busy);
    progress.classList.toggle('hidden', !busy);
}

function setActionsVisible(visible) {
    actions.hidden = !visible;
}

function showHarness(url) {
    if (!url || enteringHarness) return;

    enteringHarness = true;
    harnessURL = url;
    harnessFrame.addEventListener('load', () => {
        harnessFrame.hidden = false;
        splash.hidden = true;
    }, { once: true });
    harnessFrame.src = url;
}

// WKWebView can occasionally restore a stale compositor surface after a
// minimise/unminimise cycle. Keeping the Wails document as the top-level page
// preserves the runtime bindings; touching the iframe layer requests a repaint
// without reloading DSH or losing its in-memory UI state.
function refreshHarnessLayer() {
    if (!harnessURL || harnessFrame.hidden) return;
    harnessFrame.classList.remove('repaint');
    void harnessFrame.offsetWidth;
    harnessFrame.classList.add('repaint');
}

function handleStatus(s) {
    if (!s) return;

    switch (s.state) {
        case 'ready':
            statusText.textContent = s.message || 'DeepSeek Harness 已就绪，正在进入…';
            setBusy(false);
            setActionsVisible(false);
            // Briefly let the "ready" state paint before revealing the embedded UI.
            setTimeout(() => showHarness(s.url), 300);
            break;

        case 'starting':
            statusText.textContent = s.message || '正在启动…';
            setBusy(true);
            setActionsVisible(false);
            break;

        default: // error | exited
            statusText.textContent = s.message || '启动失败';
            setBusy(false);
            setActionsVisible(true);
            break;
    }
}

document.getElementById('btn-retry').addEventListener('click', () => App.Retry());
document.getElementById('btn-node').addEventListener('click', () => App.OpenNodeJS());
document.getElementById('btn-browser').addEventListener('click', () => App.OpenInBrowser());
document.getElementById('btn-quit').addEventListener('click', () => App.Quit());
document.getElementById('btn-logs').addEventListener('click', async () => {
    try {
        const logs = await App.Logs();
        logsEl.hidden = false;
        logsEl.textContent = logs || '(无日志)';
    } catch (err) {
        console.error(err);
    }
});

runtime.EventsOn('status', handleStatus);
App.Status().then(handleStatus).catch((err) => console.error(err));

window.addEventListener('focus', refreshHarnessLayer);
window.addEventListener('pageshow', refreshHarnessLayer);
document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') refreshHarnessLayer();
});
