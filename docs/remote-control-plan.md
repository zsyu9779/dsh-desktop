# DSH Desktop 远程控制方案（对标 Codex · 含移动端 App）

> 状态：技术方案 v1（待 review / 拍板）
> 分支：`feature/phone-remote`
> 关联文档：`docs/product-spec.md`（产品方向）、`docs/remote-access.md`（M1 现状）、`agent_context.md`（交接上下文）
> 本文回答一个问题：**在 M1 局域网远程已经跑通的基础上，怎么把它做成像 Codex 一样「真远程 + 移动端 App」的完整方案。**

---

## 1. 现状分析（feature/phone-remote 分支，M1）

### 1.1 已实现的能力

M1 已经把「手机浏览器远程操控桌面上的 dsh」跑通了，核心是 **Go 反向代理 + token 鉴权 + 二维码配对**：

```
手机浏览器(PWA)
   │  http://<LAN-IP>:8787/?t=<token>     ← 明文 HTTP，token 在 URL
   ▼
remote.go 反向代理（0.0.0.0:8787）
   │  校验 token → 改写 Host/Origin/Referer 为 loopback → 注入 polyfill → 转发
   ▼
dsh web  @ 127.0.0.1:<随机端口>           ← 安全边界不变
```

关键实现点（都是真问题、真修复，见 `agent_context.md` §5）：

| 修复 | 解决什么 | 代码位置 |
|---|---|---|
| 改写 `req.Host` 为 loopback | dsh `/api` trust 栅栏把手机 Host 当外人 → 全 403 | `remote.go` `proxy.Director` |
| 改写 `Origin`/`Referer` 为 loopback | dsh 要求 `Origin === Host` | 同上 |
| 注入 `crypto.randomUUID` polyfill | 明文 LAN 是非安全上下文，该 API 缺失导致前端抛错 | `remote_polyfill.js` |
| 强制 browse 目录选择器 | native picker 被钉死在 loopback，手机 403 | `dsh.go` `augmentedEnv()` 注入 `SSH_CONNECTION` |
| 二维码优先物理网卡 | 避免 VPN/VM 网卡（utun/vmnet）抢 IP | `remote.go` `firstLANIP()` |
| 常驻宿主面板 | 不再启动即重定向退场，可开关远程 | `frontend/` |

### 1.2 现状评价

**做对了的**：定位准确（真痛点是「agent 跑到一半需要人」而非「打洞」）；安全边界清晰（loopback 不变，靠反代改写）；WebSocket/SSE 流式转发直接复用 `httputil.ReverseProxy`，无需重写。

**M1 的边界就是「局域网 + 可信环境」**。以下 10 个问题决定它现在还「不是 Codex 级的远程控制」，也决定后续阶段要补什么：

### 1.3 问题与风险清单（按严重度排序）

| # | 问题 | 严重度 | 影响 | 对应阶段 |
|---|---|---|---|---|
| 1 | **单 token = 完全控制权**，且 token 在 URL 明文传输，进浏览器历史 / 代理日志；无 per-device 身份、无单独吊销 | 高 | 泄一次 token = 永久后门；无法区分设备 | M1.5 |
| 2 | **明文 HTTP**，局域网内可被嗅探 / MITM，token 与内容全裸 | 高 | 咖啡厅/公司 WiFi 不安全 | M1.5 |
| 3 | 反代把 Host/Origin 全改成 loopback → **`PRIVILEGED_METHODS`（settings.* / credentials.* / agentPreset.*）全放行** | 高 | 手机拿 token 后权限等同坐在电脑前 | 设计上接受但需显式授权 |
| 4 | 无**推送通知**：手机浏览器关掉就失联，agent 卡在审批没人知道 | 高 | 直接违背核心价值「审批 + 通知」 | M2/M3 |
| 5 | 无**公网通道**：人不在同一 WiFi 就废了 | 高 | 只能同屋用 | M3 |
| 6 | `ModifyResponse` 的 HTML 注入**未处理 Content-Encoding**：一旦 dsh 给 HTML 开 gzip/brotli，会在压缩字节上注入导致页面损坏 | 中 | 上游升级即坏 | M1.5 |
| 7 | 无连接状态 / 设备列表 / 断开能力，无法看到「谁连上了」 | 中 | 无掌控感、难排查 | M1.5 |
| 8 | 二维码只显示单个 IP，多网卡可能选错；无 mDNS/Bonjour 让手机自动发现 | 低 | 偶发连不上 | M1.5 |
| 9 | `wails dev` 热重载硬杀应用，留孤儿 dsh 进程 | 低 | 开发体验 | 工具层 |
| 10 | 会话状态只在 host 上，无多设备同步语义（M1 天然单设备） | 中 | 换设备看不到同状态 | M3 |

---

## 2. 对标 Codex：它到底是怎么「远程」的

Codex 的远程访问（2025 年起并入 ChatGPT App）不是「电脑开个端口让手机连」，而是**账号中继模型**：

```
ChatGPT 移动 App（iOS/Android）
   ↕  TLS + 账号鉴权 + 会话路由 API + APNs/FCM 推送
OpenAI 云端（账号 / 中继 / 推送）
   ↕  TLS WebSocket，由主机【主动出站】建立，绑定账号+设备
codex CLI（跑在你的电脑上，主机即 server）→ 你的文件 / 仓库 / 凭据
```

四个关键属性（这是「像不像 Codex」的分水岭）：

1. **主机只出站、不开入站端口**——NAT/防火墙友好，天然适配家用网络，无需端口映射。
2. **账号绑定 + 会话在主机（主机即 server）**——换手机登录同一账号，路由到同一批主机，看到主机上正在跑的会话；云端不托管被控制的会话状态。
3. **推送通知是第一公民**——审批/提问/完成/报错实时推到手机，点通知直达会话。
4. **移动端是「克制」的完整体验**——查看线程状态、读 diff/终端、回复提问、审批命令、改方向/换模型、新建任务；大 diff 审阅、密钥、部署回电脑。

> 我们 M1 只实现了第 4 条的「能连上」这一层，且是局域网直连。**差距不在传输，在「通知闭环 + 会话连续性 + 账号/设备管理」。**（与 `product-spec.md` §15 的结论一致）

社区对照实现（可参考其 WebSocket 桥接做法）：[gronxb/codex-relay](https://github.com/gronxb/codex-relay)（电脑上跑 WS 服务、手机 PWA 连，真工作留电脑上）、[K9i-0/ccpocket](https://github.com/K9i-0/ccpocket)（Codex/Claude 的 WebSocket 桥）。

---

## 3. 目标架构

把「局域网反代」升级成「局域网直连（免费） + 公网中继（订阅） + 移动端 App」三层：

```
                    ┌──────────────────────────────────────────────┐
                    │            移动端（PWA 先行 / 原生兜底）          │
                    │  iOS: SwiftUI + WKWebView + APNs + Keychain     │
                    │  Android: WebView + FCM  (或 Capacitor/TWA)      │
                    │  PWA: Web Push + SW + Install                     │
                    └───────────────┬──────────────────────────────────┘
                            LAN 直连 │         │ 公网（relay + E2E）
                            HTTPS/WS │         │ TLS WS（手机→relay→主机）
                                     ▼         ▼
┌─────────────────────────────────────────────────────────────────────┐
│  桌面壳（常驻宿主，本仓库）                                             │
│                                                                      │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────┐  ┌──────────┐ │
│  │ 设备/配对管理 │  │ LAN 反代(:8787)│  │ relay 客户端    │  │ 推送桥接   │ │
│  │ device 注册表│  │ HTTPS+证书指纹│  │ (出站 WS, M3)  │  │ 事件→APNs │ │
│  │ 密钥对/吊销  │  └──────┬───────┘  └──────┬────────┘  │ /FCM/WebP │ │
│  └──────┬──────┘         │                │            └──────────┘ │
│         │                └───────┬────────┴─────────────┐            │
│         │                        ▼                       │            │
│         │              dsh web @ 127.0.0.1:<port>  ← 安全边界不变      │
│         │                        ▲                       │            │
│         └────────── 事件订阅（SSE/WS）────────────────────┘            │
└─────────────────────────────────────────────────────────────────────┘
                    ┌──────────────────────────────────────────────┐
                    │  服务端（M3 起，仅订阅档）                        │
                    │  relay（密文透传）+ 账号 + 设备 + 推送 + 订阅/支付  │
                    └──────────────────────────────────────────────┘
```

**不变式**：dsh 仍只监听 `127.0.0.1`；所有入站（LAN / 公网）都先经过宿主壳的鉴权与加密层；任务、文件、凭据、会话**永不离开宿主机**。

---

## 4. 关键技术设计

### 4.1 配对与身份：从「单 token」到「每设备凭据」

M1 的 `?t=<token>` 是「一把万能钥匙」。升级为**每设备身份 + 可单独吊销**：

1. 宿主首启生成一对密钥（Ed25519/X25519）+ 一个短期 CA，用于后续签发设备凭据与 LAN 证书。
2. **扫码配对改成一次性配对码**：二维码编码 `配对码(短、单次、TTL 60s) + 宿主公钥 + LAN/relay 地址`。配对码只用于这一次「交换公钥/建立信任」，不是会话凭据 → 从根上消除「token 常驻 URL」问题。
3. 配对时手机端生成自己的密钥对，与宿主完成握手（PAKE 或签名交换），宿主在**设备注册表**里登记：`deviceID / 设备名 / 公钥指纹 / 签发时间 / 最近活跃`。
4. 会话凭据用**短时 JWT（宿主签发，含 deviceID + scope）**，放 HttpOnly + Secure + SameSite cookie；支持 per-device 吊销（踢掉某台手机不影响其它）。
5. scope 化（可选增强）：默认授予「会话/审批/任务」读写；`settings.* / credentials.* / agentPreset.*` 等 `PRIVILEGED_METHODS` 默认**拒**，需在桌面端显式勾选「允许远程管理敏感设置」才放行——把 M1「改写 loopback 导致全放行」变成显式能力。

> 落地文件：新增 `pairing.go`（密钥/CA/注册表/JWT）、`remote.go` 改造鉴权中间件、`app.go` 增 `ListDevices/RevokeDevice` 绑定、`frontend` 增设备面板。

### 4.2 局域网传输：明文 HTTP → HTTPS + 证书指纹

- LAN 反代改走 **HTTPS**，证书由宿主自签 CA 签发（SAN 含 LAN IP 与 `dsh-desktop.local`）。
- 二维码携带**证书指纹**，手机首次配对时 TOFU（Trust On First Use）固定该指纹，之后指纹变了（证书轮换）就告警 → 消除 MITM 与 token 嗅探。
- 保留 `httputil.ReverseProxy` 承载 HTTP + WebSocket + SSE；**修复 Content-Encoding**：在 `Director` 里 `req.Header.Set("Accept-Encoding", "identity")`，或正确解压→注入→重压，避免上游开 gzip 时损坏页面。
- 可选加 mDNS（`dsh-desktop.local`）让手机自动发现，解决「二维码只给一个 IP」的边界情况。

### 4.3 公网传输：出站 relay + E2E（M3，订阅档）

这是「从局域网迈向真远程」的关键一步，架构对齐 Codex：

- **主机出站、不入站**：宿主内跑一个 relay 客户端，启动即向我们的 relay 服务器建立一条持久 TLS WebSocket（携带账号凭据 + 主机 ID）。家用 NAT / 防火墙无需任何配置。
- **relay 只做密文透传**：手机 ↔ 宿主之间的业务载荷用双方 E2E 会话密钥（X25519 派生的对称密钥）加密，relay 只能看到「哪个账号 ↔ 哪台主机」的路由信息，看不到内容（也是合规免责点，见 `product-spec.md` §7.4）。
- **多路复用**：一条 relay 连接上按 `sessionID` 多路复用 HTTP/WS/SSE 通道，实现「手机通过 relay 完整驱动 dsh web」。
- **降级策略**：优先 LAN 直连（免费、低延迟）；LAN 不通自动回落到 relay（订阅）；连接状态在宿主角落清晰展示。

> 可选开源对照：`gronxb/codex-relay` 的 WS 桥、frp / Tailscale 的 NAT 穿透思路；但产品定位是「零配置 + 完整体验」，所以 relay 是我们的托管服务而非让用户自己搭。

### 4.4 推送通知桥接（M2/M3，灵魂能力）

Codex 的灵魂是「agent 卡住时手机能响」。实现分两步：

1. **事件订阅**：宿主订阅 dsh 的事件流（dsh 已有 SSE/WebSocket 会话事件），识别四类高价值事件：`ask-user 提问`、`plan-mode 审批`、`任务完成`、`任务报错`。
2. **投递**：
   - PWA：Web Push（需 relay/推送服务中转，Web Push 需要 HTTPS）。
   - 原生 iOS：APNs（必须原生，这是「原生不可省」的硬理由）；Android：FCM。
   - 通知携带**深链**：点通知直达对应会话/审批页。

> 该能力是免费/订阅的分界线之一（免费 LAN 档只有「前台网页可见」，订阅档才有真推送），与 `product-spec.md` §5 一致。

### 4.5 移动端 App：PWA 先行 + 原生壳兜底

| 形态 | 什么时候 | 干什么 | 位置 |
|---|---|---|---|
| **PWA** | 现在（M1 已可用） | 复用 dsh Web UI，加 Service Worker / Web Push / install prompt | 无需新仓（dsh 已是 PWA） |
| **原生 iOS** | M4 | SwiftUI + WKWebView 复载 dsh UI；原生只做 4 件事：APNs token、Keychain 存设备密钥、universal link 深链、FaceID 解锁 | 独立仓库 |
| **原生 Android** | M4 | WebView + FCM（或 Capacitor/TWA）；职责同 iOS | 独立仓库 |

**为什么原生壳是「薄壳」而非重写**：dsh Web UI 已是完整移动体验（`product-spec.md` §8 逐条映射过），重写体验层成本高且追不上上游迭代；薄壳只补「推送 + 安全存储 + 深链」这三个 PWA 补不上的原生能力。

### 4.6 会话连续性与多设备同步（M3）

- **宿主是唯一真相源**：所有 session 都在 `~/.dsh`，手机只是远程视图 → 天然「多设备看到同一份状态」，不需要像 Codex 那样做云端会话复制。
- 宿主把状态/事件**广播**给所有已配对设备（经 LAN 直连或 relay），实现「桌面派任务 → 手机跟进 → 回电脑终审」的闭环。

---

## 5. 分阶段实施计划

| 阶段 | 目标 | 核心工作 | 验收标准 |
|---|---|---|---|
| **M1.5 安全加固（本仓库）** | 把「能用」变「敢用」 | 每设备凭据 + 配对码 + 设备注册表/吊销；LAN HTTPS + 证书指纹；修 Content-Encoding；设备面板 UI | 1) 多台手机分别配对、单独吊销互不影响 2) LAN 全程 HTTPS、改指纹会告警 3) 上游开 gzip 页面不损坏 |
| **M2 移动体验 + 通知（PWA）** | 审批/通知闭环的 PWA 版 | 事件订阅 + Web Push + 深链；移动端审批/任务/工作区体验打磨 | 手机锁屏也能收到「待审批」Web Push，点通知直达审批页 |
| **M3 公网 + 服务端（订阅）** | 人在外面也能远程 | relay（出站 WS + 密文透传）+ 账号 + 设备管理 + APNs/FCM + 订阅/支付 + E2E | 手机切 4G 仍能完整操控；relay 日志看不到业务内容 |
| **M4 原生 App** | 补 iOS 推送最后一公里 | iOS/Android 薄壳（WKWebView/WebView + 推送 + Keychain + 深链） | App Store/TestFlight 版本：扫码配对 → 收 APNs → 深链直达 |

**本仓库（dsh-desktop）范围内要做的是 M1.5 + M2 的宿主侧**；M3 服务端、M4 原生壳独立建仓。

### 本仓库具体改造点（M1.5/M2）

| 文件 | 改动 |
|---|---|
| 新增 `pairing.go` | 密钥对/CA、一次性配对码、设备注册表、JWT 签发与校验 |
| 改 `remote.go` | 鉴权中间件（token→JWT + per-device）、LAN HTTPS、修 `Accept-Encoding`、暴露设备/连接状态 |
| 改 `dsh.go` | 订阅 dsh 事件流，产出「待审批/提问/完成/报错」事件 |
| 新增 `notify.go` | 事件 → Web Push（M2）→ APNs/FCM（M3）桥接 |
| 改 `app.go` | 绑定 `Pair/ListDevices/RevokeDevice`、事件 `remote-devices` 推送 |
| 改 `frontend/` | 常驻面板加「设备列表 + 连接状态 + 吊销」；配对二维码改为配对码流程 |

---

## 6. 安全模型与风险

| 风险 | 应对 |
|---|---|
| token/内容在 LAN 明文被嗅探 | M1.5 上 HTTPS + 证书指纹 TOFU |
| token 泄露 = 永久后门 | 一次性配对码 + 短时 JWT + per-device 吊销 |
| `PRIVILEGED_METHODS` 被远程滥用 | scope 化，敏感方法默认拒、桌面端显式授权 |
| relay 侧泄密 / 合规 | E2E 密文透传，服务端只见路由不见内容 |
| relay 滥用 / 成本 | 限速、封禁、ToS（见 `product-spec.md` §13） |
| 官方 dsh 原生做远程 | 差异化押「体验层 + 通知闭环 + 跨 agent 统一入口」，不押传输 |
| 上游 HTML 结构变化 | 注入改为更健壮的解析 + `Accept-Encoding: identity` |

---

## 7. 待拍板（阻塞实现的决策）

> 需求澄清轮（grill-with-docs）已敲定的决策见 `CONTEXT.md` + `docs/adr/`，关键结论：
> - **主机即 server**，会话/文件/凭据永远在主机，云端账号不托管会话状态（ADR-0001）。
> - **免费局域网免账号**（每设备扫码配对）；账号只在订阅档，做身份/路由/推送/设备注册表（ADR-0002）。
> - 公网传输 = **纯 relay**（E2E 密文）；一个账号 = **多台主机 + 多台设备**；移动端 **v1 薄壳复用 dsh Web UI**（纯原生重写留作后续）。

仍待拍板：

1. **M1.5 是否先做**（纯本仓库、无外部依赖、立刻可做），还是直接跳到 M3 公网？——建议 M1.5 先行，安全债不欠。
2. **relay 服务端技术选型**：自研 Go relay vs 基于 frp/Tailscale 二次封装——决定 M3 周期。
3. 主力市场（国内/海外）与订阅定价——决定支付与推送通道（`product-spec.md` §12 遗留）。

---

## 8. 结论

M1 已经证明了「反代 + 改写 + 常驻宿主」这条技术路线成立，且安全边界（loopback 不变）设计正确。下一步不是推翻它，而是按 **M1.5 安全加固 → M2 通知闭环 → M3 公网 relay → M4 原生壳** 的顺序，逐步把「局域网能连」补成「Codex 级的真远程 + 移动端 App」：差异不在传输，在**每设备身份与吊销、通知闭环、会话连续性**。
