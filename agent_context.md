# Agent Context（多 session 交接上下文）

> 本文件记录 dsh-desktop 项目「手机远程操控」这一工作线的完整上下文，供后续 session（人或 agent）快速接续。
> 最后更新：M1 局域网远程跑通时。

---

## 1. 项目是什么

- **dsh-desktop**：用 Wails（Go + 系统原生 WebView）把官方 `@deepseek-ai/dsh` 的 Web UI 包成桌面壳（类似 Codex 桌面版）。
- **产品方向**：做成「像 Codex 一样用手机远程操控」的 app。核心卖点是**完整体验**（审批 + 通知 + 多设备同步），不是「打洞」。
- 详细产品方案见 `docs/product-spec.md`，Codex 体验对标基准也在其中。

---

## 2. 当前状态

- **分支**：`feature/phone-remote`（从 main 切出）。
- **M1（局域网手机远程）已完成并跑通**。
- 提交链（9 个，从旧到新）：
  - `b8c139a` 产品方案文档
  - `47a43cf` Codex 体验对标基准
  - `98f901f` M1 主体（反代 + 二维码 + 常驻宿主）
  - `9f87f5e` 依赖标准化（go-qrcode 改标准 go get）
  - `85f3e92` 网卡选择（优先物理网卡）
  - `f35da1e` Origin/Referer 重写
  - `6ed4326` crypto.randomUUID polyfill
  - `8eed67b` 强制 browse 目录选择器
  - `4a43f45` Host 头重写（真根因）+ 重新生成的 bindings
- **未提交的用户 WIP（不要碰）**：`README.md`（用户重写定位文案）、`README.en.md`（未跟踪）。

---

## 3. 关键文件

- `remote.go` — 反向代理核心：token 鉴权、二维码、Host/Origin/Referer 改写、polyfill 注入、物理网卡选择。
- `remote_polyfill.js` — `crypto.randomUUID` polyfill（`//go:embed` 嵌入）。
- `app.go` — App 绑定方法：`EnableRemote/DisableRemote/RemoteStatus/RegenerateRemoteToken`。
- `dsh.go` — dsh 进程生命周期；`augmentedEnv()` 注入 `SSH_CONNECTION=dsh-desktop` 强制 browse 目录选择器。
- `frontend/src/main.js` + `frontend/index.html` — 常驻宿主（启动后不重定向，显示「进入桌面界面」+「手机远程」面板）。
- `docs/product-spec.md` — 产品方案 + 对标基准。
- `docs/remote-access.md` — M1 远程功能使用/架构/限制。

---

## 4. 如何运行

```
wails dev     # 开发模式（热重载前端 + Go）
wails build   # 打包 build/bin/dsh-desktop.app
```

- dsh 优先绑 `3080`，被占用则随机端口。
- 远程代理端口固定 `8787`（被占用则随机）。
- `wails dev` 热重载 Go 会**硬杀**应用（不走 shutdown 清理），留孤儿 dsh 进程（`npx → npm exec → node dsh`），需手动 kill。

---

## 5. M1 远程的架构与三个关键修复（最重要）

架构：手机浏览器 → 桌面壳内反向代理（8787，token 鉴权）→ 改写 Host/Origin → dsh web（127.0.0.1）。

三个关键修复（缺一不可，否则手机 403/报错）：

1. **改写 Host 头**：`httputil.NewSingleHostReverseProxy` 只改 `req.URL.Host`、**不改 `req.Host`**（后者才是真正发出的 Host 头）。必须显式 `req.Host = targetURL.Host`。否则 dsh `/api` trust 栅栏把手机 Host（192.168.x.x）当外人 → 全 403。
2. **改写 Origin/Referer**：dsh 要求 `Origin` 与 `Host` 一致，否则 403。
3. **注入 crypto.randomUUID polyfill**：该 API 只在安全上下文（HTTPS/localhost）存在，手机走明文 LAN IP 时是 undefined，dsh 前端一发起 API 请求就抛错。

外加：
- 强制 browse 目录选择器（`SSH_CONNECTION=dsh-desktop`），否则 native 选择器（`host.pickDirectory`，loopback 特权）403。
- 二维码优先物理网卡（`en*/eth*`），避免抓到 utun/vmnet。

---

## 6. dsh 内部机制与坑（查源码时很有用）

- dsh 源码位置（已安装的 npm 包，可直接读）：`/Users/zhangshiyu/.dsh-desktop/npm-cache-v1/_npx/6c7f445d1bf61956/node_modules/@deepseek-ai/`
- `dsh web` = `--profile web` 别名；`dsh-host-webserver` 只允许 host `127.0.0.1` 或 `0.0.0.0`。
- **`--host 0.0.0.0` 被官方硬拒**（`dsh-web-app/lib/startup.js`）：「would expose remote code execution to the network」。所以不能直接绑公网，只能走反代。
- `/api` trust 栅栏（`dsh-client-connection/lib/index.js` 的 `isTrustedApiRequest`）：Host 必须 loopback 或受信；Origin 必须匹配 Host；`sec-fetch-site` 不能是 cross-site。
- `PRIVILEGED_METHODS`（loopback 钉死）：`agentPreset.*`、`host.pickDirectory`、`host.openPath`、`settings.*`、`credentials.*`、`llm.discoverModels`。**注意**：我们反代把 Host+Origin 都改成 loopback 后，这些也全放行了 → token = 完全控制权。
- `crypto.randomUUID` 是 secure-context-only（`dsh-host-apiproxy/lib/types/fetch/client.js` 用它生成 rpcId）。
- 目录选择器（`dsh-host-directory-picker-auto`）：loopback+darwin → native（`host.pickDirectory`，特权）；检测到 `SSH_CONNECTION`/`SSH_TTY` → browse（`host.listDirectory`，非特权）。
- 存储：`~/.dsh/`（`DSH_HOME` 默认），含 `sessions/`、`storages/workspace.json`（工作区列表）、`storages/session_projcache.json`。
- **会话按 workspace 目录分桶**：`~/.dsh/sessions/<cwd 编码>/`（如 `/Users/zhangshiyu/dsh-desktop` → `--Users-zhangshiyu-dsh-desktop--`）。桌面壳默认 `cmd.Dir = workspaceDir()`（`DSH_WORKSPACE` 或用户主目录）。

---

## 7. 环境事实

- 机器：macOS（arm64），Xcode 26.3，Go 1.26.0，Node v25.8.2。
- dsh 版本：`@deepseek-ai/dsh@0.1.0-rc.6`（`dsh.go` 的 `dshPackage` 常量固定）。
- 端口占用：`3080` = 当前 agent session 的 harness（勿杀）；`8787` = 远程代理；`34115` = wails dev server；`5173` = vite。
- 日志：`~/.dsh-desktop/logs/dsh.log`。
- 桌面壳工作目录：默认用户主目录，可用 `DSH_WORKSPACE` 覆盖，`DSH_HOME` 控制 profiles/存储位置。

---

## 8. 决策记录

- 手机端：**PWA 先行，原生不可省**（iOS 推送必须原生 APNs）；原生工程独立建仓，不进本仓库。
- 付费：**局域网免费 / 公网订阅**；卖「零配置 + 完整体验」，不卖打洞。
- E2E：**信誉卖点，放免费档**，不作付费门槛；扫码即完成密钥交换。
- 北极星：**对标 Codex 体验**——审批 + 通知是灵魂，会话连续性 + 通知闭环是差距所在。

---

## 9. 下一步（待办）

- **M2 移动端体验层**：审批 / 提问 / 任务在手机端的体验打磨。
- **iOS 壳工程**（独立仓库）：扫码配对 + WKWebView + 预留 APNs 推送。
- 收尾：把「手机远程」写进 README（等用户 README WIP 完成后）。
- 清理：`wails dev`（job bash-4）还挂着，需要时 kill。

---

## 10. 重要提醒（给下一个 session）

- `README.md` / `README.en.md` 是用户未提交 WIP，**编辑/提交时务必跳过**。
- 改 Go 代码后 `wails dev` 会自动重编译 + 重启 app，**远程 token 会变**，手机需重新扫码。
- 杀进程时**别误杀 3080 端口的 harness**（那是当前 agent session 自己）。
- 本 session 早期沙箱拦过 go module 下载，后来用户开了 danger-full-access；现在 `go get` / `go build` 正常。
- 提交时用 `git add <具体文件>`，别用 `git add -A`（会带上用户的 WIP）。
