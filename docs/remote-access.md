# 手机远程访问（M1 · 局域网）

> 状态：M1 已跑通（同 WiFi 局域网）。公网档（M3）见 `product-spec.md`。

## 是什么

把桌面端 `dsh web`（跑在 `127.0.0.1` 上）通过一个**带 token 鉴权的反向代理**暴露到局域网，让手机浏览器扫码即可操控这台电脑上的 DeepSeek Harness。

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
手机浏览器(PWA)
   │  http://<LAN-IP>:8787/?t=<token>
   ▼
桌面壳内反向代理（remote.go，Go）
   │  校验 token → 改写 Host/Origin/Referer 为 loopback → 注入 polyfill → 转发
   ▼
dsh web  @ 127.0.0.1:<随机端口>   ← 安全边界不变
```

### 反向代理做的三件关键事

1. **改写 Host 头为 loopback**：`NewSingleHostReverseProxy` 只改 `req.URL.Host`、不改 `req.Host`，必须显式 `req.Host = targetURL.Host`。否则 dsh 的 `/api` trust 栅栏会把手机当外人，全 403。
2. **改写 Origin/Referer 为 loopback**：dsh 要求 `Origin` 与 `Host` 一致，否则 403。
3. **注入 crypto.randomUUID polyfill**：该 API 只在安全上下文（HTTPS/localhost）存在，手机走明文 LAN IP 没有它，dsh 前端发起 API 请求会直接抛错。

### 其他改动

- **强制 browse 目录选择器**：给 dsh 进程注入 `SSH_CONNECTION=dsh-desktop`，让目录选择走「browse」（API 列目录，远程可用）而非「native」（弹 macOS 原生选择框，被钉死在 loopback）。
- **二维码**：编码 `http://<LAN-IP>:8787/?t=<token>`，扫码即完成「配对」（token 写进 HttpOnly cookie）。
- **优先物理网卡**：二维码取局域网 IP 时优先 `en*/eth*`，避免抓到 VPN（utun）/虚拟机（vmnet）网卡。

## 安全边界（重要）

- **token = 完全控制权**。请求被完整重写成 loopback，手机能调用包括 `settings.*`、`credentials.*` 在内的所有方法——和坐在电脑前等价。
- M1 是**局域网明文 HTTP**，token 随 URL 走。仅适合可信局域网；不要直接暴露到公网。
- 可随时点「重新生成配对码」让旧 token 失效。
- 公网档（M3）会在此之上叠 E2E + 账号，见 `product-spec.md`。

## 已知限制（M1）

- 只支持同一局域网（同一 WiFi）。
- 桌面端目录选择器从「原生 macOS 对话框」降级为「browse 网页列目录」——为保手机也能用，是刻意的取舍。
- 二维码只显示一个局域网 IP；多网卡极端情况可能选错（已优先物理网卡）。
- `wails dev` 热重载 Go 会硬杀应用，孤儿 dsh 进程需手动清理（生产打包版走正常 shutdown 清理）。

## 相关代码

- `remote.go` — 反向代理 + token + 二维码 + Host/Origin 改写 + polyfill 注入。
- `remote_polyfill.js` — crypto.randomUUID polyfill（`//go:embed` 嵌入）。
- `app.go` — EnableRemote/DisableRemote/RemoteStatus/RegenerateRemoteToken 绑定方法。
- `dsh.go` — `augmentedEnv()` 注入 `SSH_CONNECTION=dsh-desktop`。
- `frontend/src/main.js` + `index.html` — 常驻宿主 + 「手机远程」面板。
