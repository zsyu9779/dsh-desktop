# Agent Context（多 session 交接上下文）

> 本文件记录 dsh-desktop 项目「手机远程操控」这一工作线的完整上下文，供后续 session（人或 agent）快速接续。
> 最后更新：M1.5 安全加固完成时。

---

## 1. 项目是什么

- **dsh-desktop**：用 Wails（Go + 系统原生 WebView）把官方 `@deepseek-ai/dsh` 的 Web UI 包成桌面壳（类似 Codex 桌面版）。
- **产品方向**：做成「像 Codex 一样用手机远程操控」的 app。核心卖点是**完整体验**（审批 + 通知 + 多设备同步），不是「打洞」。
- 详细产品方案见 `docs/product-spec.md`，Codex 体验对标基准也在其中。

---

## 2. 当前状态

- **分支**：`feature/phone-remote`（从 main 切出）。
- **M1（局域网手机远程）已完成并跑通**（见 §5）。
- **M1.5（安全加固）已完成**（6 张 ticket 全绿，见 §5.5）。提交链（从旧到新）：
  - `52aeead` chore: 忽略 `.scratch/`（本地 tracker）与 `.gocache/`
  - `3fcced1` ticket 01 一次性配对码 + 每设备 JWT
  - `8165536` ticket 02 Content-Encoding（gzip）修复
  - `03e298c` ticket 03 设备注册表 + 吊销 + 列表
  - `fc8ecc8` ticket 04 scope 化（敏感方法默认拒）
  - `fdeee10` ticket 05 LAN HTTPS + 证书指纹
  - `af7038d` ticket 06 设备面板 UI
  - `cea893c` docs: product-spec 回滚（跨 agent 否决 + PWA 仅验证）
- **验证**：`go test ./...` 14 个用例全过；`cd frontend && npm run build`（vite）成功。
- **未跟踪的本地文档**（按用户隐私要求保持未提交，是否提交由用户定）：`CONTEXT.md`、`docs/adr/`、`docs/agents/`、`AGENTS.md`、`docs/remote-control-plan.md`。
- **可删残留**：`session-522.jsonl.zstd.orig.bak`（未跟踪，非本工作线产物）。

---

## 3. 关键文件

- `remote.go` — 反向代理核心：配对码 + JWT 鉴权、设备吊销、scope 化、HTTPS（自签 CA 签发的 leaf 证书 + 指纹）、gzip Content-Encoding 注入、Host/Origin/Referer 改写、物理网卡选择。
- `pairing.go` — 每设备凭据：Ed25519 密钥对 + 短期 CA 生成/持久化、设备注册表（register/list/revoke/touch）、JWT 签发/校验、leaf 证书签发（issueLeafCert）。
- `remote_polyfill.js` — `crypto.randomUUID` polyfill（`//go:embed` 嵌入）。
- `app.go` — App 绑定：`EnableRemote/DisableRemote/RemoteStatus/RegenerateRemoteToken/ListDevices/RevokeDevice/SetAllowPrivileged`。
- `dsh.go` — dsh 进程生命周期；`augmentedEnv()` 注入 `SSH_CONNECTION=dsh-desktop` 强制 browse 目录选择器。
- `frontend/src/main.js` + `frontend/index.html` — 常驻宿主 + 手机远程面板（二维码 / 设备列表 + 吊销 / 证书指纹 / Host 公钥 / 敏感权限开关）。
- 测试文件：`remote_test.go`（配对/JWT）、`remote_content_test.go`（gzip 注入）、`remote_registry_test.go`（吊销/列表）、`remote_scope_test.go`（scope）、`remote_https_test.go`（证书指纹）。**单一 seam**：反代 upstream 边界（httptest 假上游）。
- `.scratch/phone-remote/` — 本地 issue tracker（`spec.md` + `issues/01~06`），已 gitignore。
- `docs/product-spec.md`、`docs/remote-control-plan.md`、`docs/remote-access.md` — 产品 / 技术 / 使用文档。

---

## 4. 如何运行

```
wails dev     # 开发模式（热重载前端 + Go）
wails build   # 打包 build/bin/dsh-desktop.app
```

- dsh 优先绑 `3080`，被占用则随机端口。
- 远程代理端口固定 `8787`（被占用则随机），现已走 HTTPS。
- `wails dev` 热重载 Go 会**硬杀**应用（不走 shutdown 清理），留孤儿 dsh 进程（`npm → pnpm → node → dsh`），需手动 kill。
- 改前端后记得重新打 dist：`cd frontend && npm run build`。
- 在受限沙箱里跑 Go 时若报 `operation not permitted`（写不进 `~/Library/Caches/go-build`），用 `GOCACHE=$PWD/.gocache go test ./...` 重定向到工作区。

---

## 5. M1 远程的架构与三个关键修复（历史）

架构：手机浏览器 → 桌面壳内反向代理（8787，token 鉴权）→ 改写 Host/Origin → dsh web（127.0.0.1）。

三个关键修复（缺一不可，否则手机 403/报错）：

1. **改写 Host 头**：`httputil.NewSingleHostReverseProxy` 只改 `req.URL.Host`、**不改 `req.Host`**（后者才是真正发出的 Host 头）。必须显式 `req.Host = targetURL.Host`。否则 dsh `/api` trust 栅栏把手机 Host 当外人 → 全 403。
2. **改写 Origin/Referer**：dsh 要求 `Origin` 与 `Host` 一致，否则 403。
3. **注入 crypto.randomUUID polyfill**：该 API 只在安全上下文（HTTPS/localhost）存在，手机走明文 LAN IP 时是 undefined，dsh 前端一发起 API 请求就抛错。

外加：强制 browse 目录选择器（`SSH_CONNECTION=dsh-desktop`）、二维码优先物理网卡（`en*/eth*`）。

---

## 5.5 M1.5 安全加固（M1 之上的新层）

M1 的「单 token + 明文 HTTP + 全权限」升级为：

- **每设备 JWT**：Host 首启生成 Ed25519 密钥对 + 短期 CA，持久化到 `~/.dsh-desktop/`（可用环境变量 `DSH_DESKTOP_STATE` 覆盖）。二维码放**一次性配对码**（TTL 60s、单次），`/?pair=<code>` 完成 Pairing → 登记 Device → 签发短时 JWT（HttpOnly cookie，含 deviceID + scope）。
- **吊销**：设备注册表（`devices.json`）支持 list/revoke/touch；authMiddleware 每次校验 JWT 签名 + 设备仍存在（吊销即 403）+ 刷新 lastActive。
- **scope 化**：敏感方法（dsh `PRIVILEGED_METHODS` 全集，见 §6）默认对手机 403，桌面端 `SetAllowPrivileged(true)` 才放行。判定点：URL path `/api/<method>` 是否命中 `remote.go` 的 `privilegedMethods` map。
- **HTTPS**：反代监听改 TLS，证书由 Host CA 签发的 leaf 证书（SAN = dsh-desktop.local + LAN IP），二维码/状态暴露证书 **SHA-256 指纹**供手机 TOFU 固定。
- **Content-Encoding**：Director 请求 `Accept-Encoding: identity`；ModifyResponse 对 gzip 做「解压 → 注入 polyfill → 重压」，避免上游开 gzip 时页面损坏。
- 不变式仍成立：dsh 只监听 loopback，所有入站先过宿主壳鉴权 + 加密层；任务 / 文件 / 凭据 / 会话永不离开 Host。

---

## 6. dsh 内部机制与坑（查源码时很有用）

- dsh 源码位于 pnpm 的内容寻址 store，哈希目录不稳定；可用 `find ~/.dsh-desktop/pnpm-store-v1 -path '*/links/@deepseek-ai/dsh/0.1.0-rc.8/*/node_modules/@deepseek-ai/dsh'` 定位当前包，不要硬编码 dlx/store 哈希。
- `dsh web` = `--profile web` 别名；`dsh-host-webserver` 只允许 host `127.0.0.1` 或 `0.0.0.0`。
- **`--host 0.0.0.0` 被官方硬拒**（`dsh-web-app/lib/startup.js`）：「would expose remote code execution to the network」。所以不能直接绑公网，只能走反代。
- `/api` trust 栅栏（`dsh-client-connection/lib/index.js` 的 `isTrustedApiRequest`）：Host 必须 loopback 或受信；Origin 必须匹配 Host；`sec-fetch-site` 不能是 cross-site。
- `PRIVILEGED_METHODS`（`dsh-client-connection/lib/index.js`，**精确集合非通配**）：`agentPreset.read/copy/openDocument/remove`、`host.pickDirectory`、`host.openPath`、`settings.describe/openDocument/update/replace/mutate`、`credentials.describe/set/unset`、`llm.discoverModels`。反代把 Host+Origin 改成 loopback 后这些本会全放行——**M1.5 已在反代层做 scope 化**（见 §5.5），`remote.go` 的 `privilegedMethods` map 与此一致。
- 方法名在 URL path：`/api/<method>`（`pathname.slice(5)`），不是 JSON 体。
- `crypto.randomUUID` 是 secure-context-only（`dsh-host-apiproxy/lib/types/fetch/client.js` 用它生成 rpcId）。
- 目录选择器（`dsh-host-directory-picker-auto`）：loopback+darwin → native（`host.pickDirectory`，特权）；检测到 `SSH_CONNECTION`/`SSH_TTY` → browse（`host.listDirectory`，非特权）。
- 存储：`~/.dsh/`（`DSH_HOME` 默认），含 `sessions/`、`storages/workspace.json`、`storages/session_projcache.json`。
- **会话按 workspace 目录分桶**：`~/.dsh/sessions/<cwd 编码>/`。桌面壳默认 `cmd.Dir = workspaceDir()`（`DSH_WORKSPACE` 或用户主目录）。

---

## 7. 环境事实

- 机器：macOS（arm64），Xcode 26.3，Go 1.26.0，Node v25.8.2。
- dsh 版本：`@deepseek-ai/dsh@0.1.0-rc.8`（`dsh.go` 的 `dshPackage` 常量固定）。
- 端口占用：`3080` = 当前 agent session 的 harness（勿杀）；`8787` = 远程代理（HTTPS）；`5173` = vite。
- 日志：`~/.dsh-desktop/logs/dsh.log`。
- 桌面壳工作目录：默认用户主目录，可用 `DSH_WORKSPACE` 覆盖，`DSH_HOME` 控制 profiles/存储位置。
- **沙箱**：本 session 早期是 workspace-write，Go 默认构建缓存写不进 → 用 `GOCACHE=$PWD/.gocache`；后半程用户已改 danger-full-access（无限制）。
- **`wails` CLI 不在 PATH**：本 session 未跑 wails，所以 `frontend/wailsjs/` 的生成绑定未刷新——`App.js`/`App.d.ts`/`models.ts` 缺 `ListDevices/RevokeDevice/SetAllowPrivileged`，`remoteStatus` 字段还是旧的（带 `token`）。`main.js` 用的是运行时绑定 `window.go.main.App`，功能不受影响；下次 `wails build`/`wails dev` 会自动重新生成。

---

## 8. 决策记录

- 手机端：**原生 app = 唯一交付物**；PWA 仅作内部验证工具（不对外交付）。原生工程独立建仓，不入本仓库（ADR-0003）。
- 付费：**局域网免费 / 公网订阅**；卖「零配置 + 完整体验」，不卖打洞。
- E2E：**信誉卖点，放免费档**，不作付费门槛；扫码即完成密钥交换。
- **跨 agent 统一入口：已否决**——v1 仅适配 dsh，不做 Claude Code / Codex。
- **scope 化**：敏感方法默认对手机拒，桌面端显式授权。
- 市场：都做、**国内先行、iOS 先行**（APNs + Apple 订阅一次接入全球）；上架地区（国内 ICP 备案 vs 海外）**延后到 M3 门槛前再拍板**。
- 北极星：**对标 Codex 体验**——审批 + 通知是灵魂，会话连续性 + 通知闭环是差距所在。
- 域词汇（CONTEXT.md）：Host / Device / Pairing / Session / Goal / Task / Agent（v1=仅 dsh）/ Model provider / Owner。

---

## 9. 下一步（待办）

- **M2 移动端体验层**：审批 / 提问 / 任务在手机端体验打磨（注意：PWA 现在是验证工具，不是交付里程碑）。
- **iOS 壳工程**（独立仓库）：扫码配对 + WKWebView + 预留 APNs；订阅（Apple IAP）在这里收钱。
- **M3 公网**（服务端独立建仓）：relay（出站 WS + E2E）+ 账号 + 订阅 + 推送。门槛前要拍板：上架地区/主体/ICP、登录方式（Sign in with Apple 等）、定价锚点、relay 选型。
- 收尾：README 已在 M1 时提交，M1.5 无需改；删掉 `session-522.jsonl.zstd.orig.bak` 残留。

---

## 10. 重要提醒（给下一个 session）

- 提交用 `git add <具体文件>`，别 `git add -A`；`.scratch/` 与 `.gocache/` 已 gitignore。
- 改 Go 代码后 `wails dev` 会自动重编译 + 重启 app，**远程配对码会变**，手机需重新扫码。
- 杀进程时**别误杀 3080 端口的 harness**（那是当前 agent session 自己）。
- **Go 项目验证 = `gofmt -l` / `go build` / `go vet` / `go test`**；`verify` skill 是 React 专属（yarn/linc/flow），对 Go 不适用。
- 生成绑定（`frontend/wailsjs/`）在 `wails build`/`wails dev` 时自动刷新；手改 main.js 后跑 `cd frontend && npm run build` 重打 dist。
- 未跟踪的本地文档（`CONTEXT.md`、`docs/adr/`、`docs/agents/`、`AGENTS.md`、`docs/remote-control-plan.md`）按用户隐私要求保持未提交；是否提交/推送由用户定。
