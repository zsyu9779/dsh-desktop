# 手机远程访问（M1.5 · 局域网）

> 状态：M1.5 已跑通（同 WiFi 局域网，HTTPS、设备 Pairing 与可撤销授权）。公网档（M3）见 `product-spec.md`。

## 是什么

把 Host 上的 `dsh web`（跑在 `127.0.0.1` 上）通过一个**带设备鉴权的 HTTPS 反向代理**暴露到局域网，让 Device 浏览器扫码完成 Pairing 后操控 Host 上的 DeepSeek Harness。

- 手机端**无需装 app**：dsh 前端本身是 PWA，扫二维码 → 浏览器打开 → 「添加到主屏幕」即可。
- 任务、文件、凭据、会话都**留在电脑上**，手机只是远程操控界面。

## 怎么用

1. 桌面端启动（`wails dev` 或打包后的 app），等状态变「已就绪」。
2. 点「手机远程」→「开启」→ 出现二维码。
3. **手机和电脑连同一 WiFi**，用手机相机扫二维码 → 用浏览器打开。
4. iOS：Safari 分享按钮 → 「添加到主屏幕」→ 变成全屏 app 图标。
5. 手机上即可查看工作区、会话历史、收发消息、审批、切换目录。

## 架构

```
Device 浏览器(PWA)
   │  https://<LAN-IP>:8787/?pair=<一次性配对码>
   ▼
桌面壳内反向代理（remote.go，Go）
   │  消费配对码/校验设备 JWT → 改写 Host/Origin/Referer 为 loopback → 注入 polyfill → 转发
   ▼
dsh web  @ 127.0.0.1:<随机端口>   ← 安全边界不变
```

### 反向代理做的三件关键事

1. **改写 Host 头为 loopback**：`NewSingleHostReverseProxy` 只改 `req.URL.Host`、不改 `req.Host`，必须显式 `req.Host = targetURL.Host`。否则 dsh 的 `/api` trust 栅栏会把手机当外人，全 403。
2. **改写 Origin/Referer 为 loopback**：dsh 要求 `Origin` 与 `Host` 一致，否则 403。
3. **注入 crypto.randomUUID polyfill**：为不完整支持该 API 的 WebView/浏览器提供兼容实现，避免 dsh 前端发起 API 请求时抛错。

### 其他改动

- **强制 browse 目录选择器**：给 dsh 进程注入 `SSH_CONNECTION=dsh-desktop`，让目录选择走「browse」（API 列目录，远程可用）而非「native」（弹 macOS 原生选择框，被钉死在 loopback）。
- **二维码**：编码 `https://<LAN-IP>:8787/?pair=<一次性配对码>`；配对码 60 秒有效且只能消费一次，成功后签发 HttpOnly 设备 JWT。
- **设备管理**：已配对 Device 会写入 Host 的设备注册表；轮换配对码不会撤销它们，需在设备列表中逐个撤销。
- **HTTPS**：Host 为当前局域网地址签发自签名证书，并在界面展示证书指纹供 Owner 核对。
- **优先物理网卡**：二维码取局域网 IP 时优先 `en*/eth*`，避免抓到 VPN（utun）/虚拟机（vmnet）网卡。

## 安全边界（重要）

- **设备凭据代表远程控制权**。默认敏感方法受 Host 侧授权开关限制；Owner 开启敏感权限后，已配对 Device 才能调用 `settings.*`、`credentials.*` 等方法。
- M1.5 使用**局域网自签名 HTTPS**与设备级凭据。仅适合可信局域网；不要直接暴露到公网。
- 「重新生成配对码」只让尚未消费的旧配对码失效；撤销既有 Device 必须使用设备列表中的撤销操作。
- 公网档（M3）会在此之上叠 E2E + 账号，见 `product-spec.md`。

## 已知限制（M1）

- 只支持同一局域网（同一 WiFi）。
- 桌面端目录选择器从「原生 macOS 对话框」降级为「browse 网页列目录」——为保手机也能用，是刻意的取舍。
- 二维码只显示一个局域网 IP；多网卡极端情况可能选错（已优先物理网卡）。
- `wails dev` 热重载 Go 会硬杀应用，孤儿 dsh 进程需手动清理（生产打包版走正常 shutdown 清理）。

## 相关代码

- `remote.go` — HTTPS 反向代理 + 一次性配对码 + 设备 JWT + 二维码 + Host/Origin 改写 + polyfill 注入。
- `remote_polyfill.js` — crypto.randomUUID polyfill（`//go:embed` 嵌入）。
- `app.go` — EnableRemote/DisableRemote/RemoteStatus/RegenerateRemoteToken 绑定方法。
- `dsh.go` — `augmentedEnv()` 注入 `SSH_CONNECTION=dsh-desktop`。
- `frontend/src/main.js` + `index.html` — 常驻宿主 + 「手机远程」面板。
