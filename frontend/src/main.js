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
const remoteMeta = document.getElementById('remote-meta');
const remoteHostKey = document.getElementById('remote-host-key');
const remoteCertFp = document.getElementById('remote-cert-fp');
const remoteAllowPrivileged = document.getElementById('remote-allow-privileged');
const remoteDevices = document.getElementById('remote-devices');
const remoteDeviceList = document.getElementById('remote-device-list');
const accountRemote = document.getElementById('account-remote');
const accountRemoteMessage = document.getElementById('account-remote-message');
const accountRemoteSetup = document.getElementById('account-remote-setup');
const accountRemoteQR = document.getElementById('account-remote-qr');
const accountRemoteExpiry = document.getElementById('account-remote-expiry');
const accountRemoteDevices = document.getElementById('account-remote-devices');
const accountRemoteDeviceList = document.getElementById('account-remote-device-list');
const lanRegistration = document.getElementById('lan-registration');
const lanRegistrationList = document.getElementById('lan-registration-list');

let remoteEnabled = false;
let remoteSetupPoll = null;
let accountSignedIn = false;

// --- DSH version update check ---
const updateEl = document.getElementById('update');
const updateCurrent = document.getElementById('update-current');
const updateHint = document.getElementById('update-hint');
const updateLatest = document.getElementById('update-latest');
const btnCheckUpdate = document.getElementById('btn-check-update');
const btnUpdate = document.getElementById('btn-update');

let updateBusy = false;

function renderUpdate(info) {
    if (!info) return;
    updateCurrent.textContent = info.current || '—';
    updateEl.hidden = false;
    if (info.error) {
        updateHint.textContent = '检查更新失败（网络不可用或 npm registry 无响应）';
        updateHint.hidden = false;
        btnUpdate.hidden = true;
        return;
    }
    if (info.hasUpdate) {
        updateLatest.textContent = info.latest;
        updateHint.textContent = '发现新版本';
        updateHint.hidden = false;
        btnUpdate.hidden = updateBusy;
    } else {
        updateHint.textContent = '已是最新版本';
        updateHint.hidden = false;
        btnUpdate.hidden = true;
    }
}

function setUpdateBusy(busy, msg) {
    updateBusy = busy;
    btnCheckUpdate.disabled = busy;
    btnUpdate.disabled = busy;
    if (msg) {
        updateHint.textContent = msg;
        updateHint.hidden = false;
    }
    if (busy) btnUpdate.hidden = true;
}

btnCheckUpdate.addEventListener('click', async () => {
    try {
        setUpdateBusy(true, '正在检查更新…');
        renderUpdate(await App.CheckDSHUpdate());
    } catch (err) {
        console.error(err);
        updateHint.textContent = '检查更新失败';
        updateHint.hidden = false;
    } finally {
        setUpdateBusy(false);
    }
});

btnUpdate.addEventListener('click', async () => {
    const target = updateLatest.textContent;
    if (!target) return;
    try {
        setUpdateBusy(true, '正在升级到 ' + target + ' 并重启…');
        await App.SetDSHVersion(target);
        renderUpdate(await App.CheckDSHUpdate());
    } catch (err) {
        console.error(err);
        updateHint.textContent = '升级失败：' + err;
        updateHint.hidden = false;
        setUpdateBusy(false);
    }
});

// --- notify subscription ---
const notifyEl = document.getElementById('notify');
const notifyList = document.getElementById('notify-list');

let notifications = [];
let notifyFilter = '';

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
    accountRemote.hidden = !visible;
}

function stopRemoteSetupPoll() {
    if (remoteSetupPoll) window.clearInterval(remoteSetupPoll);
    remoteSetupPoll = null;
}

function startRemoteSetupPoll() {
    stopRemoteSetupPoll();
    remoteSetupPoll = window.setInterval(async () => {
        try {
            handleRemoteSetup(await App.RefreshRemoteSetup());
        } catch (err) {
            console.error(err);
        }
    }, 2000);
}

function renderPairedDevices(devices) {
    accountRemoteDeviceList.innerHTML = '';
    accountRemoteDevices.hidden = !devices || devices.length === 0;
    (devices || []).forEach((device) => {
        const item = document.createElement('li');
        item.className = 'remote-device';
        item.textContent = (device.name || 'Device') + ' · ' + device.deviceID;
        accountRemoteDeviceList.appendChild(item);
    });
}

async function renderLANRegistration(pairedDevices) {
    try {
        const registered = new Set((pairedDevices || []).map((device) => device.deviceID));
        const devices = (await App.ListDevices()).filter((device) => !registered.has(device.deviceId));
        lanRegistrationList.innerHTML = '';
        lanRegistration.hidden = devices.length === 0;
        devices.forEach((device) => {
            const item = document.createElement('li');
            item.className = 'remote-device';
            const name = document.createElement('span');
            name.textContent = device.name || 'LAN Device';
            const register = document.createElement('button');
            register.className = 'btn btn-quiet';
            register.textContent = '登记';
            register.addEventListener('click', async () => {
                try {
                    handleRemoteSetup(await App.RegisterLANPairing(device.deviceId, device.name || 'LAN Device'));
                } catch (err) {
                    accountRemoteMessage.textContent = 'LAN Pairing 登记失败：' + err;
                }
            });
            item.appendChild(name);
            item.appendChild(register);
            lanRegistrationList.appendChild(item);
        });
    } catch (err) {
        console.error(err);
    }
}

function handleRemoteSetup(status) {
    if (!status) return;
    accountRemoteMessage.textContent = status.message || '';
    const pending = status.state === 'pending';
    const cancelFailed = status.state === 'cancel-failed';
    accountRemoteSetup.hidden = !pending && !cancelFailed;
    accountRemoteQR.hidden = !pending || !status.qr;
    if (status.qr) accountRemoteQR.src = status.qr;
    accountRemoteExpiry.textContent = pending && status.expiresAt
        ? '此 QR 将于 ' + new Date(status.expiresAt).toLocaleTimeString() + ' 失效'
        : '';
    const start = document.getElementById('btn-account-remote-start');
    start.textContent = pending ? '等待批准' : '设置';
    start.disabled = !accountSignedIn || pending || cancelFailed;
    document.getElementById('btn-account-remote-refresh').hidden = cancelFailed;
    renderPairedDevices(status.devices);
    renderLANRegistration(status.devices);
    if (pending) startRemoteSetupPoll(); else stopRemoteSetupPoll();
}

function handleAccountStatus(status) {
    accountSignedIn = status && status.state === 'signed-in';
    document.getElementById('btn-account-sign-in').textContent = accountSignedIn ? 'Account 已登录' : '登录 Account';
    document.getElementById('btn-account-sign-in').disabled = accountSignedIn;
    document.getElementById('btn-account-remote-start').disabled = !accountSignedIn;
    if (!accountSignedIn && status && status.message) accountRemoteMessage.textContent = status.message;
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
        renderRemoteMeta(s);
        renderDevices();
    } else {
        remoteMeta.hidden = true;
        remoteDevices.hidden = true;
    }
}

function renderRemoteMeta(s) {
    remoteMeta.hidden = false;
    remoteHostKey.hidden = !s.hostPublicKey;
    remoteCertFp.hidden = !s.certFingerprint;
    remoteHostKey.textContent = s.hostPublicKey ? 'Host 公钥: ' + s.hostPublicKey : '';
    remoteCertFp.textContent = s.certFingerprint ? '证书指纹: ' + s.certFingerprint : '';
    remoteAllowPrivileged.checked = !!s.allowPrivileged;
}

function timeAgo(t) {
    if (!t) return '—';
    const d = new Date(t);
    if (isNaN(d.getTime())) return '—';
    const diff = Date.now() - d.getTime();
    if (diff < 30000) return '在线';
    if (diff < 60000) return '刚刚';
    if (diff < 3600000) return Math.floor(diff / 60000) + ' 分钟前';
    if (diff < 86400000) return Math.floor(diff / 3600000) + ' 小时前';
    return d.toLocaleString();
}

async function renderDevices() {
    try {
        const devices = await App.ListDevices();
        remoteDevices.hidden = false;
        remoteDeviceList.innerHTML = '';
        if (!devices || devices.length === 0) {
            const li = document.createElement('li');
            li.textContent = '(暂无已配对设备)';
            remoteDeviceList.appendChild(li);
            return;
        }
        devices.forEach((d) => {
            const li = document.createElement('li');
            li.className = 'remote-device';
            const name = document.createElement('span');
            name.textContent = (d.name || '设备') + ' · ' + timeAgo(d.lastActive);
            const btn = document.createElement('button');
            btn.className = 'btn btn-quiet';
            btn.textContent = '吊销';
            btn.addEventListener('click', async () => {
                try {
                    await App.RevokeDevice(d.deviceId);
                    renderDevices();
                } catch (err) {
                    console.error(err);
                }
            });
            li.appendChild(name);
            li.appendChild(btn);
            remoteDeviceList.appendChild(li);
        });
    } catch (err) {
        console.error(err);
    }
}

function typeLabel(t) {
    switch (t) {
        case 'question': return '提问';
        case 'approval': return '待审批';
        case 'completed': return '完成';
        case 'error': return '报错';
        default: return t || '通知';
    }
}

function renderNotifications() {
    notifyList.innerHTML = '';
    const filtered = notifications.filter((n) => !notifyFilter || n.type === notifyFilter);
    filtered.forEach((n) => {
        const li = document.createElement('li');
        li.className = 'notify-item' + (n.read ? ' is-read' : '');
        const badge = document.createElement('span');
        badge.className = 'notify-badge notify-badge-' + (n.type || '');
        badge.textContent = typeLabel(n.type);
        const text = document.createElement('span');
        text.className = 'notify-text';
        text.textContent = n.summary || '';
        li.appendChild(badge);
        li.appendChild(text);
        li.addEventListener('click', () => {
            n.read = true;
            renderNotifications();
            if (n.deepLink) showHarness(n.deepLink);
        });
        notifyList.appendChild(li);
    });
    if (filtered.length === 0) {
        const empty = document.createElement('li');
        empty.className = 'notify-empty';
        empty.textContent = '(暂无通知)';
        notifyList.appendChild(empty);
    }
}

function handleNotification(n) {
    if (!n) return;
    notifications.unshift(Object.assign({}, n, { read: false }));
    notifications.sort((a, b) => (b.ts || 0) - (a.ts || 0));
    if (notifications.length > 50) notifications.length = 50;
    renderNotifications();
    notifyEl.hidden = false;
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
            App.RemoteSetupStatus().then(handleRemoteSetup).catch((err) => console.error(err));
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

document.getElementById('btn-account-remote-start').addEventListener('click', async () => {
    try {
        handleRemoteSetup(await App.StartRemoteSetup());
    } catch (err) {
        accountRemoteMessage.textContent = 'Remote setup 失败：' + err;
    }
});

document.getElementById('btn-account-sign-in').addEventListener('click', async () => {
    try {
        handleAccountStatus(await App.SignInAccount());
        if (accountSignedIn) handleRemoteSetup(await App.RemoteSetupStatus());
    } catch (err) {
        accountRemoteMessage.textContent = 'Account 登录失败：' + err;
    }
});

document.getElementById('btn-account-remote-refresh').addEventListener('click', async () => {
    try {
        handleRemoteSetup(await App.RefreshRemoteSetup());
    } catch (err) {
        accountRemoteMessage.textContent = '刷新失败：' + err;
    }
});

document.getElementById('btn-account-remote-cancel').addEventListener('click', async () => {
    try {
        handleRemoteSetup(await App.CancelRemoteSetup());
    } catch (err) {
        accountRemoteMessage.textContent = '已在本机取消；server 同步失败：' + err;
        handleRemoteSetup(await App.RemoteSetupStatus());
    }
});

remoteAllowPrivileged.addEventListener('change', async () => {
    try {
        await App.SetAllowPrivileged(remoteAllowPrivileged.checked);
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

document.getElementById('btn-notify-clear').addEventListener('click', () => {
    notifications.length = 0;
    renderNotifications();
    notifyEl.hidden = true;
});

document.querySelectorAll('.notify-filter-btn').forEach((btn) => {
    btn.addEventListener('click', () => {
        notifyFilter = btn.dataset.type || '';
        document.querySelectorAll('.notify-filter-btn').forEach((b) => b.classList.toggle('is-active', b === btn));
        renderNotifications();
    });
});

runtime.EventsOn('status', handleStatus);
runtime.EventsOn('remote', handleRemote);
runtime.EventsOn('dsh-update', renderUpdate);
runtime.EventsOn('notifications', handleNotification);
App.Status().then(handleStatus).catch((err) => console.error(err));
App.AccountStatus().then(handleAccountStatus).catch((err) => console.error(err));
App.DSHVersion().then((v) => { updateCurrent.textContent = v || '—'; }).catch(() => {});

window.addEventListener('focus', refreshHarnessLayer);
window.addEventListener('pageshow', refreshHarnessLayer);
document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') refreshHarnessLayer();
});
