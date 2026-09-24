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
- **scope 化**：DSH 0.1.2 删除了上游 `PRIVILEGED_METHODS`，改由壳自己定策略（默认拒绝 + 白名单）。判定点：URL path `/api/<namespace>/<method>` 是否命中 `remote.go` 的 `remoteAllowedEndpoints` / `remoteAllowedExactPaths`；桌面端 `SetAllowPrivileged(true)` 语义变为「全部放行」。
- **HTTPS**：反代监听改 TLS，证书由 Host CA 签发的 leaf 证书（SAN = dsh-desktop.local + LAN IP），二维码/状态暴露证书 **SHA-256 指纹**供手机 TOFU 固定。
- **Content-Encoding**：Director 请求 `Accept-Encoding: identity`；ModifyResponse 对 gzip 做「解压 → 注入 polyfill → 重压」，避免上游开 gzip 时页面损坏。
- 不变式仍成立：dsh 只监听 loopback，所有入站先过宿主壳鉴权 + 加密层；任务 / 文件 / 凭据 / 会话永不离开 Host。

---

## 6. dsh 内部机制与坑（查源码时很有用）

- dsh 源码位于 pnpm 的内容寻址 store，哈希目录不稳定；可用 `find ~/.dsh-desktop/pnpm-store-v1 -path '*/links/@deepseek-ai/dsh/0.1.7-rc.1/*/node_modules/@deepseek-ai/dsh'` 定位当前包，不要硬编码 dlx/store 哈希。
- `dsh web` = `--profile web` 别名；`dsh-host-webserver` 只允许 host `127.0.0.1` 或 `0.0.0.0`。
- **0.1.5 起 CLI 入口被 `import.meta.main` 守卫**（`apps/cli/src/bin.ts`）：该特性 Node.js 22.18 / 24.2 才有，在 **24.0/24.1 上整个 CLI 会静默退出**——退出码 0、stdout 无任何输出，壳只会白等到 `readyTimeout` 超时。所以 `isSupportedNodeVersion` 对 24 线要求 minor ≥ 2。
  - 已实测（用壳完全相同的 CLI 参数直接跑 `lib/bin.js`）：Node 24.0.0 与 24.1.0（`import.meta.main === undefined`）× dsh 0.1.5-rc.1 → **exit 0、stdout 0 字节**；同样两个 Node × dsh 0.1.2-rc.1 → 正常打印 URL，说明问题由 0.1.5 引入而非 Node 本身；Node 24.19.0（`true`）× 0.1.5-rc.1 → 正常打印 URL。
- **旧 WebKit 缺 `Iterator` 全局**：0.1.5 的 `ui-sidebar-documentpreview` 打包产物在模块作用域求值 `Iterator.prototype.join`，WebKit < Safari 18.4（macOS < 15.4）直接 `Can't find variable: Iterator`，整个 client module 导入失败。壳的处理**不是**禁用该模块，而是用预装的 `dsh-webview-compat`（host-only）经 `webserver/index-inject` 往页面注入一段垫片：`Iterator` 缺失时才用 `Object.getPrototypeOf(Object.getPrototypeOf([][Symbol.iterator]()))` 取到真正的 `%IteratorPrototype%` 再定义 `globalThis.Iterator`——必须挂在真正的原型上，否则库自己补的 `join` 落不到任何迭代器上。注入行渲染在 client module 入口之前（但排在 bundle 注册脚本之后；factory 是延迟执行的，所以够早）。
- **Wails 的 macOS WebView 打不开新窗口**：Wails v2.11.0 darwin 的 `WailsContext` 声明了 `WKUIDelegate`，却没有实现 `webView:createWebViewWithConfiguration:`（Linux/Windows 侧亦然，见上游 issue #1522）。于是 DSH 会话里渲染成 `<a target="_blank">` 的外链（`dsh-web-frontend` 打包产物 `m5`/`_5` 只对 http(s) 加 `target="_blank"`）点击后**无声无息**：不跳转、不报错。壳侧没有导航钩子，DSH 页面又在独立 loopback origin 上、壳的文档脚本够不到，所以用预装的 `dsh-webview-links`（host-only，`webserver/index-inject`）注入一段脚本：仅当 `window.parent !== window`（即被壳 iframe 嵌入；浏览器直开/手机远程是顶层，保持原生行为）时接管 `a[target=_blank]` 的 http(s) 点击，`preventDefault` 后 `postMessage` 给父页；`frontend/src/main.js` 校验 `event.source === harnessFrame.contentWindow` 与 origin 后调用 `App.OpenExternalURL`，Go 侧只放行 http(s) 再走 `runtime.BrowserOpenURL`（与壳的「在浏览器打开」同一路径）。协议串 `dsh-desktop:open-external` 由 `TestWebviewLinksProtocolMatchesShell` 跨文件钉住。
- **`--host 0.0.0.0` 被官方硬拒**（`dsh-web-app/lib/startup.js`）：「would expose remote code execution to the network」。所以不能直接绑公网，只能走反代。
- `/api` trust 栅栏（`dsh-client-connection/lib/index.js` 的 `isTrustedApiRequest`）：Host 必须 loopback 或受信；Origin 必须匹配 Host；`sec-fetch-site` 不能是 cross-site。
- **0.1.2 起上游已无 `PRIVILEGED_METHODS`**：`/api` 只有「信任围栏 + 浏览器会话 cookie」（`dsh-client-connection` 的 `isTrustedApiRequest` + `BrowserAuth`），敏感面的收敛下移到客户端 `ctx.connection.isLoopback`（按 `location.hostname` 判断，仅 UI 层）。因此手机侧权限**由壳的 allowlist 独占**（见 §5.5 与 `remote.go`）。
- 方法名在 URL path：`/api/<method>`（`pathname.slice(5)`），不是 JSON 体。
- `crypto.randomUUID` 是 secure-context-only（`dsh-host-apiproxy/lib/types/fetch/client.js` 用它生成 rpcId）。
- 目录选择器（`dsh-host-directory-picker-auto`）：loopback+darwin → native（`host.pickDirectory`，特权）；检测到 `SSH_CONNECTION`/`SSH_TTY` → browse（`host.listDirectory`，非特权）。
- 存储：`~/.dsh/`（`DSH_HOME` 默认），含 `sessions/`、`storages/workspace.json`、`storages/session_projcache.json`。
- **会话按 workspace 目录分桶**：`~/.dsh/sessions/<cwd 编码>/`。桌面壳默认 `cmd.Dir = workspaceDir()`（`DSH_WORKSPACE` 或用户主目录）。

---

## 7. 环境事实

- 机器：macOS（arm64），Xcode 26.3，Go 1.26.0，Node v25.8.2。
- dsh 版本：`@deepseek-ai/dsh@0.1.7-rc.1`（`dsh.go` 的 `dshPackage` 常量固定；运行时可用 `~/.dsh-desktop/config.json` 的 `dshVersion` 覆盖）。0.1.5-rc.3 → 0.1.7-rc.1 是破坏性版本：客户端 Session 改为多实例（`SessionStandardProps` 去掉 `nodes`/`current`，新增 `useConversation`/`useProjection`），`ToolResultNode.resultView/callView` 删除，`settings.plugin.item` 删除。预装插件需按 §11 适配。
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

---

## 11. 适配新 DSH 版本时的预装插件审计（用户要求）

**用户要求：每次让其适配最新 DSH 版本时，都必须先检查预装插件里有没有被上游替代、可以卸载的。**

固定流程（改 `dsh.go` 的 `dshPackage` 之前）：

1. 读新版本 release notes 的「新增功能 / 其他变更」，列出可能与预装插件重叠的上游原生能力；
2. 逐个核对 `preinstall.go` 的 `preinstalledPlugins`（当前：open-editor、diff-review、account-login、webview-compat、webview-links；`agent-preset-compat` 已于 2026-09-24 退役），判断上游是否已原生提供同等能力；
3. 被替代 / 不再需要的移入 `retiredPlugins`（附确切 `cordis.patch.yml` 卸载块），并更新 `README.md` / `README.en.md` 的插件清单；
4. 保留的插件核对 peer 范围能否满足新版本（semver 预发布元组规则：跨 minor 的 rc 不满足 `^0.1.5-rc.1`），不能就更新 + 重建 client bundle；
5. 审计结论记回本节。

**2026-09-24 对 0.1.7-rc.1 的审计（尚未实施，待适配）：**

- `agent-preset-compat`：**已于 2026-09-24 退役**（移入 `retiredPlugins`，`plugins/agent-preset-compat/` 已删）。peer 依赖 `@deepseek-ai/dsh-agent-presets` 已停更（最后版本 0.1.6-alpha.2），0.1.7 改用单数 `@deepseek-ai/dsh-agent-preset`，且 preset 改由插件组合包声明——与它「复制旧目录 preset」的机制冲突。
- `diff-review`：保留，但要适配两点——(a) 设置卡片注册的 `settings.plugin.item` slot 在 0.1.7 已被删除（0.1.5 有 11 处引用、0.1.7 为 0），需改到 `settings.section` / `settings.plugins.tab`；(b) 上游 0.1.7 原生接管了「会话文件改动卡片 + 侧边栏逐文件对比审阅」，与它能力重叠，是否收窄需产品判断。
- 0.1.7 的插件↔DSH 兼容校验用 `semver.satisfies(..., { includePrerelease: true })`（见 `boot/app-boot/src/plugin-compatibility.ts`），实测旧的 `^0.1.5-rc.1` / `^0.1.2-rc.1` 均**满足** `0.1.7-rc.1` → **peer 范围不用改，不会被拒**。
- `webview-compat` / `webview-links`：保留。依赖的 `webserver/index-inject` 事件在 0.1.5-rc.1 与 0.1.7-rc.1 都是 4 处引用，未变。
- `0.1.5-rc.3`（当前 latest）：仅依赖锁定 hotfix（3 commit，只改 `pnpm-lock.yaml` + `verify-package-dependencies.ts`），无 API 变更，不影响插件。

**可卸载性结论（2026-09-24 逐插件复核）：**

- `agent-preset-compat`：**已于 2026-09-24 退役**（`retiredPlugins`）。上游 0.1.7 已无 `@deepseek-ai/dsh-agent-presets`（改为单数 `@deepseek-ai/dsh-agent-preset`），preset 改由插件组合包声明，旧的 `.agent-presets/<id>/` 目录模型需迁移 → 本插件的复制机制失效。本机已确认 0 个 `agentPreset: "code"` 会话。
- `open-editor`：**不能因上游替代而卸载**。0.1.7 原生 `dsh-client-ui-open-in-app`（会话页头把 workspace 打开到已安装应用 + 预览用默认应用打开文件）已覆盖其主要能力，但 `diff-review` 依赖它的 `/open-editor/open` 路由做「在指定行打开」；只能收窄，不能移除。
- `diff-review`：**保留**。原生 `dsh-client-ui-deliverables` 只覆盖「会话文件改动」审阅；git 工作区 staged/unstaged/untracked 的 accept/revert + commit/push/PR + AI 审阅仍独有。
- `webview-compat`：**保留**。0.1.7 的 `client.pdf.js` 仍在模块级求值 `typeof Iterator.prototype.join`（`Iterator` 未定义时同样 ReferenceError）→ 旧 WebKit 仍需垫片。
- `webview-links`：**保留**。根因是 Wails WebView 无法开新窗口，与 DSH 版本无关。
- `dsh-account-login`：**保留**。桌面自有的 Host Account / Relay 登录，上游无对应物。

**待核（尚未跑 live）：** `remote.go` 的 `/api/<ns>/<method>` 精确 allowlist 与 `notify.go` 的 mux 解析是否需对齐 0.1.7 的 Remote 双向流/二进制改动。

**2026-09-24 实施与验证结果（0.1.7-rc.1）：**

- 适配完成：`dsh.go` pin → `@deepseek-ai/dsh@0.1.7-rc.1`；`diff-review` 用本地源码（`~/dsh-plugin-src/dsh-plugin-diff-review`）移植到 0.1.7 并 vendor（版本 0.1.2，重建 `client.js` + `dist/index.js`，client build 把 `@deepseek-ai/dsh-client-store` 设为 external）。
- 收窄：会话变更交由上游 `@deepseek-ai/dsh-client-ui-deliverables` 原生实现；diff-review 保留 git 工作区审阅 + 评论 dock + review-package 渲染 + `settings.plugins.tab`。放弃了「选中文本加入对话」入口（0.1.7 的 `SessionListState` 无 `current`，root scope 的 `shell.overlay` 无 `sessionId`）。
- 验证证据：`npx tsc --noEmit` 0 错误、`npm run build` 通过；桌面 `go build` / `go test` 全绿；隔离 `DSH_HOME` 下真跑 `dsh@0.1.7-rc.1` 到达 ready，Chrome DevTools 打开 UI **零插件控制台错误**（移植前是 `list slot "conversation.chat.turnTail" requires options.id`）。
- 兼容性校验用 `includePrerelease: true`，旧 peer 范围无需改；所有 `dsh.client.inject` 包在 0.1.7 均存在。
- 未覆盖：diff-review 工作区审阅的实际点选交互（只验证了加载与注册无错）；`remote.go`/`notify.go` 走通用代理路径，未单独回归。
