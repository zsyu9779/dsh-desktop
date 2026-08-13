import './style.css';

// Wails v2 injects the runtime and bound Go methods as window globals.
const runtime = window.runtime;
const App = window.go.main.App;

const statusText = document.getElementById('status-text');
const spinner = document.getElementById('spinner');
const progress = document.getElementById('progress');
const actions = document.getElementById('actions');
const logsEl = document.getElementById('logs');

function setBusy(busy) {
    spinner.classList.toggle('hidden', !busy);
    progress.classList.toggle('hidden', !busy);
}

function setActionsVisible(visible) {
    actions.hidden = !visible;
}

function handleStatus(s) {
    if (!s) return;

    switch (s.state) {
        case 'ready':
            statusText.textContent = s.message || 'DeepSeek Harness 已就绪，正在进入…';
            setBusy(false);
            setActionsVisible(false);
            // Briefly let the "ready" state paint before handing off the window.
            setTimeout(() => window.location.replace(s.url), 300);
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
