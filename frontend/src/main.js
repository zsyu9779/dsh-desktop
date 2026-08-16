import './style.css';

// Wails v2 injects the runtime and bound Go methods as window globals.
const runtime = window.runtime;
const App = window.go.main.App;

const statusText = document.getElementById('status-text');
const spinner = document.getElementById('spinner');
const progress = document.getElementById('progress');
const actions = document.getElementById('actions');
const actionsReady = document.getElementById('actions-ready');
const remoteEl = document.getElementById('remote');
const logsEl = document.getElementById('logs');
const splash = document.getElementById('splash');
const harnessFrame = document.getElementById('harness-frame');

let harnessURL = '';
let enteringHarness = false;

const btnRemoteToggle = document.getElementById('btn-remote-toggle');
const remoteDetail = document.getElementById('remote-detail');
const remoteQR = document.getElementById('remote-qr');
const remoteURL = document.getElementById('remote-url');

let remoteEnabled = false;

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

function setReadyUI(visible) {
    actionsReady.hidden = !visible;
    remoteEl.hidden = !visible;
}

function handleRemote(s) {
    if (!s) return;
    remoteEnabled = !!s.enabled;
    btnRemoteToggle.textContent = remoteEnabled ? '关闭' : '开启';
    remoteDetail.hidden = !remoteEnabled;
    if (remoteEnabled) {
        const pairingUrl = s.url ? (s.url + '/?pair=' + s.pairingCode) : '';
        remoteURL.textContent = pairingUrl || s.url || '';
        if (s.qr) {
            remoteQR.src = s.qr;
            remoteQR.hidden = false;
        } else {
            remoteQR.hidden = true;
        }
    }
}

function handleStatus(s) {
    if (!s) return;

    switch (s.state) {
        case 'ready':
            statusText.textContent = s.message || 'DeepSeek Harness 已就绪';
            setBusy(false);
            setActionsVisible(false);
            setReadyUI(true);
            document.getElementById('btn-enter').onclick = () => showHarness(s.url);
            App.RemoteStatus().then(handleRemote).catch((err) => console.error(err));
            break;

        case 'starting':
            statusText.textContent = s.message || '正在启动…';
            setBusy(true);
            setActionsVisible(false);
            setReadyUI(false);
            break;

        default: // error | exited
            statusText.textContent = s.message || '启动失败';
            setBusy(false);
            setActionsVisible(true);
            setReadyUI(false);
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

btnRemoteToggle.addEventListener('click', async () => {
    try {
        if (remoteEnabled) {
            await App.DisableRemote();
            handleRemote(await App.RemoteStatus());
        } else {
            handleRemote(await App.EnableRemote());
        }
    } catch (err) {
        console.error(err);
    }
});

document.getElementById('btn-remote-regen').addEventListener('click', async () => {
    try {
        handleRemote(await App.RegenerateRemoteToken());
    } catch (err) {
        console.error(err);
    }
});

document.getElementById('btn-remote-copy').addEventListener('click', async () => {
    const text = remoteURL.textContent;
    if (!text) return;
    try {
        await navigator.clipboard.writeText(text);
    } catch (err) {
        const range = document.createRange();
        range.selectNodeContents(remoteURL);
        const sel = window.getSelection();
        sel.removeAllRanges();
        sel.addRange(range);
        document.execCommand('copy');
        sel.removeAllRanges();
    }
});

runtime.EventsOn('status', handleStatus);
runtime.EventsOn('remote', handleRemote);
App.Status().then(handleStatus).catch((err) => console.error(err));

window.addEventListener('focus', refreshHarnessLayer);
window.addEventListener('pageshow', refreshHarnessLayer);
document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') refreshHarnessLayer();
});
