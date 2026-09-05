package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the root Wails application. It owns the DeepSeek Harness process.
type App struct {
	ctx              context.Context
	dsh              *dshManager
	remote           *remoteManager
	notify           *notifyManager
	account          *accountManager
	remoteSetup      *remoteSetupManager
	relay            *relayHost
	transport        *deviceTransportTracker
	entitlement      *entitlementManager
	developerAccount bool
}

// NewApp creates a new App instance.
func NewApp() *App {
	a := &App{}
	a.transport = newDeviceTransportTracker(time.Now)
	a.dsh = newDSHManager(a)
	a.remote = newRemoteManager(a)
	a.remote.transport = a.transport
	a.notify = newNotifyManager(a)
	baseURL := accountServerURL()
	accountServer := newHTTPAccountServer(baseURL)
	secrets := keyringAccountSecretStore{}
	authorizer := accountAuthorizer(newBrowserAccountAuthorizer(baseURL, func(url string) error {
		if a.ctx == nil {
			return fmt.Errorf("Host 尚未启动")
		}
		runtime.BrowserOpenURL(a.ctx, url)
		return nil
	}))
	if developerAuthorizer, ok := loadLocalDeveloperAccountAuthorizer(); ok {
		authorizer = developerAuthorizer
		a.developerAccount = true
	}
	a.account = newAccountManager(
		authorizer,
		accountServer,
		secrets,
	)
	a.remoteSetup = newRemoteSetupManager(a.account, newHTTPRemoteSetupServer(accountServer), secrets, time.Now, a.remote)
	relayIdentity := accountRelayIdentitySource{account: a.account, pairings: a.remoteSetup}
	a.relay = newRelayHost(
		relayIdentity,
		newWebsocketRelayHostConnector(relayServerURL()),
		newDSHRelayUpstream(func() string { return a.dsh.current().URL }),
	)
	// 通知桥复用 Host Relay 身份：去重后的 Notification 派生为面向每个目标 Device
	// 的加密 envelope（投递由 dsh-server 侧 ticket 21 与跨仓 harness ticket 24 负责）。
	a.notify.bridge = &notificationBridge{identity: relayIdentity}
	a.relay.onStatus = func(s relayStatus) {
		a.emit("relay", s)
		a.refreshAccountBridgeStatus()
	}
	a.relay.onDeviceActive = func(activity deviceActivity) {
		a.transport.mark(activity)
	}
	a.relay.onRegistrySync = func(_ uint64, revokedPairingIDs []string, pairings []relayPairingSync) {
		a.remoteSetup.applyPairingSync(pairings)
		removed := a.remoteSetup.applyRevocations(revokedPairingIDs)
		for _, device := range removed {
			lanDeviceID := device.LANDeviceID
			if lanDeviceID == "" {
				lanDeviceID = device.DeviceID
			}
			a.remote.revokeDevice(lanDeviceID)
		}
		if len(removed) > 0 || len(pairings) > 0 {
			a.emit("remote-setup", a.remoteSetup.status())
		}
	}
	a.transport.onChange = func(list []deviceActivity) {
		a.emit("devices", list)
	}
	// Account、Pairing 和 Relay 是当前开发者主链路，不在 Host 本地受商业化
	// 状态拦截。entitlement 状态机保留但暂不接入生产组合；nil relayAllowed
	// 表示 Relay 始终放行。
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

	// 恢复的 keyring credential 先与 server 对账。server 重启会使短期
	// credential 失效；确认后才恢复 Relay，避免 UI 假登录和无限 401 重连。
	if a.account.currentStatus().State == accountStateSignedIn {
		go a.resumeRestoredAccount(ctx)
	} else if a.developerAccount {
		go func() {
			status := a.SignInAccount()
			a.refreshAccountBridgeStatus()
			a.emit("account", status)
		}()
	}
	// 启动即写入一次账号/Relay 状态，插件首页即可反映登录态。
	a.refreshAccountBridgeStatus()
	// dsh 设置页登录插件（邮箱/密码注册登录）经本地会话桥把凭据交回桌面壳。
	// 会话文件带启动期随机密钥的 HMAC 写方认证（见 accountBridgeSecret）。
	go watchAccountBridge(ctx, a, accountBridgeSecret())

	// Background update check: query the npm registry for a newer DSH release
	// and push the result to the splash screen without blocking startup.
	go func() {
		info := a.CheckDSHUpdate()
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "dsh-update", info)
		}
		if info.HasUpdate {
			a.dsh.logf("发现新的 DSH 版本: %s (当前 %s)", info.Latest, info.Current)
		}
	}()
}

func (a *App) resumeRestoredAccount(appCtx context.Context) {
	delay := time.Second
	for {
		validationCtx, cancel := context.WithTimeout(appCtx, 5*time.Second)
		status := a.account.validateRestoredCredential(validationCtx)
		cancel()
		a.refreshAccountBridgeStatus()
		a.emit("account", status)
		if status.State == accountStateSignedIn {
			a.relay.start()
			if a.entitlement != nil {
				a.entitlement.start()
			}
			return
		}
		if status.State != accountStateValidating {
			return
		}
		timer := time.NewTimer(delay)
		select {
		case <-appCtx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
}

// shutdown is called when the app is about to exit.
func (a *App) shutdown(ctx context.Context) {
	a.remote.disable()
	a.relay.stop()
	a.notify.stop()
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

// SignInAccount opens the system browser and completes Host Account sign-in.
func (a *App) SignInAccount() accountStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	status := a.account.signIn(ctx)
	// 重新登录成功后恢复出站 Relay 连接（SignOutAccount 曾将其停止），并订阅
	// entitlement stream。
	a.startRelayForSignedInAccount()
	a.refreshAccountBridgeStatus()
	return status
}

// startRelayForSignedInAccount 登录成功后恢复出站 Relay 与 entitlement 订阅。
func (a *App) startRelayForSignedInAccount() {
	if a.account.currentStatus().State != accountStateSignedIn {
		return
	}
	if a.entitlement != nil {
		a.entitlement.start()
	}
	if a.relay != nil {
		a.relay.start()
	}
}

// Account bridge（dsh 设置页登录插件）采用/退出实现。

func (a *App) adoptExternalCredential(credential accountCredential) accountStatus {
	return a.account.adoptExternalCredential(credential)
}

func (a *App) onBridgeSignedIn(status accountStatus) {
	a.startRelayForSignedInAccount()
	a.refreshAccountBridgeStatus()
	a.emit("account", status)
}

func (a *App) onBridgeSignedOut() {
	status := a.SignOutAccount()
	a.refreshAccountBridgeStatus()
	a.emit("account", status)
}

func (a *App) bridgeAdoptFailed(status accountStatus) {
	a.refreshAccountBridgeStatus()
	a.emit("account", status)
}

// AccountStatus returns the current Host Account state.
func (a *App) AccountStatus() accountStatus {
	return a.account.currentStatus()
}

// RelayStatus returns the current outbound Relay connection state.
func (a *App) RelayStatus() relayStatus {
	return a.relay.status()
}

// EntitlementStatus returns the Host Account subscription entitlement state.
func (a *App) EntitlementStatus() entitlementStatus {
	if a.entitlement == nil {
		return entitlementStatus{State: entitlementUnknown, Message: "订阅状态未知"}
	}
	return a.entitlement.current()
}

// ActiveDevices 返回活动窗口内经 LAN 或 Relay 传输的 Device。
func (a *App) ActiveDevices() []deviceActivity {
	if a.transport == nil {
		return nil
	}
	return a.transport.activeDevices()
}

// SignOutAccount clears the Account credential while retaining Host and LAN identities.
func (a *App) SignOutAccount() accountStatus {
	// 先退订 entitlement（回到 unknown），再断开 Relay、清除 credential：
	// 避免退出后仍有出站连接持有已失效的 token，或残留 active 误报。
	if a.entitlement != nil {
		a.entitlement.stop()
	}
	if a.relay != nil {
		a.relay.stop()
	}
	return a.account.signOut()
}

// StartRemoteSetup creates or returns the active single-use Pairing QR.
func (a *App) StartRemoteSetup() (remoteSetupStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return a.remoteSetup.start(ctx)
}

// RefreshRemoteSetup checks whether a Device approved the active challenge.
func (a *App) RefreshRemoteSetup() (remoteSetupStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return a.remoteSetup.refresh(ctx)
}

// CancelRemoteSetup invalidates the current Pairing challenge.
func (a *App) CancelRemoteSetup() (remoteSetupStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return a.remoteSetup.cancel(ctx)
}

// RemoteSetupStatus returns the persisted Remote Pairing state.
func (a *App) RemoteSetupStatus() remoteSetupStatus {
	return a.remoteSetup.status()
}

// RegisterLANPairing registers an existing LAN Pairing using a Host identity proof.
func (a *App) RegisterLANPairing(deviceID, deviceName string) (remoteSetupStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return a.remoteSetup.registerLAN(ctx, deviceID, deviceName)
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

// UninstallPreinstalledPlugin removes a shipped plugin by id, returning whether
// it was uninstalled.
func (a *App) UninstallPreinstalledPlugin(id string) bool {
	if err := uninstallPreinstalledPlugin(id, a.dsh.logf); err != nil {
		a.dsh.logf("uninstall %s failed: %v", id, err)
		return false
	}
	return true
}

// emit 将值以指定事件名推送到前端；Host 尚未启动时静默丢弃。
func (a *App) emit(name string, value any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, value)
	}
}

// emitRemote 推送一个 remote 状态快照到前端。
func (a *App) emitRemote(s remoteStatus) {
	a.emit("remote", s)
}

// Quit exits the application.
func (a *App) Quit() {
	runtime.Quit(a.ctx)
}
