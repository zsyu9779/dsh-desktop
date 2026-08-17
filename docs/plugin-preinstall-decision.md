# 预装决策：引用 vs 自研（插件可靠性 + 安全面分析）

> 承接 `docs/plugin-research.md`。本文回答两个问题：①这三个插件靠谱吗；②直接引用还是参考自研。

---

## 一、可靠性结论（靠谱吗）

按可信度排序：

| 排名 | 插件 | 可信度 | 关键证据 |
| --- | --- | --- | --- |
| 1 | `Civitasv/dsh-plugin-diff-review` | ✅ 最靠谱 | 作者 Civitasv（130 followers、135 repos，DSH 插件生态知名作者）；TypeScript + esbuild；`execFile` 无 shell + `git -c color.ui=never` + `sanitizeRepoPath`（拒绝对/`-`/`..`）+ `validateWorkspace`/`allowedRoots` + `maxBuffer`/timeout + 跳过 symlink + 14 个测试脚本。缺陷：未发 npm、体量大（bundle 823KB / 源码 271KB tsx + 73KB server）、依赖 `dsh-plugin-open-editor`、README 写 v0.1.2 但 package.json 仍是 0.1.0 |
| 2 | `aaravarr/dsh-subagent-max` | ✅ 基本靠谱 | 对 `ctx.subagents` 的薄封装，无 shell、无网络、无遥测；已发 npm（0.1.1）。缺陷：**无自动化测试**（只有 `node --check`）、作者影响力一般（18 followers） |
| 3 | `mixin-ai/dsh-file-changes` | ⚠️ 信任度最低 | 代码干净、有测试（smoke + client）。但作者 0 followers/0 stars；reveal 端点 **origin 校验弱**（仅在有 origin 头时才校验）+ **不限工作区**（可 reveal 任意绝对路径）；未发 npm |

**共性**：三款均 MIT、均无遥测/外联网络调用（客户端 fetch 只打本地 `/api/*`）、peer 依赖都对齐 `@deepseek-ai/* ^0.1.0-rc.6` 与 cordis `^4.0.1`——与本仓库钉住的 `@deepseek-ai/dsh@0.1.0-rc.6` 一致，**今天对齐，但 rc 版本漂移风险高**。

---

## 二、影响决策的仓库事实

1. **dsh-desktop 是薄壳**：README/site 明确「会话、模型、插件和设置由上游 DeepSeek Harness 管理」，Go 侧只负责启动 `npx -y @deepseek-ai/dsh@0.1.0-rc.6 web …`、端口、就绪、日志、清理。**当前没有任何插件预装机制**。
2. **LAN 远程代理会放大插件 API 攻击面**：`remote.go` 是一个反向代理，把**所有** `/api/*` 透传到 loopback，并把 `Origin` 改写成 loopback（`targetOrigin`）。
   - 后果：若预装这三款插件，`/api/diff-review/*`（git 操作、文件读写）和 `/api/file-changes/reveal` 会经 LAN 代理可达；插件的 loopback-origin 校验被代理的 Origin 改写**抵消**，唯一门禁是配对 token（24 字节随机）。
   - 尤其 `file-changes/reveal`（可 reveal 任意绝对路径、origin 校验弱）+ `diff-review`（`allowedRoots` 默认空 = 任意 cwd 的 git/文件读写）需要收紧。

---

## 三、决策：不是二选一，分三档

### 否定项：不做「活引用 GitHub HEAD / npm range」
最差选项。原因：rc 版本漂移、作者停更/删库风险、0.1.x 已见版本号不一致、以及上面安全面问题你无法修复。

### 主策略：vendor（fork 进仓库 + 按 commit/tag 锁定 + 可 patch）
MIT 许可允许。获得：可复现、可审计、可修安全、不重写。代价：维护一个 `plugins/` 目录 + 启动时把插件装进 profile 的 bootstrap（**无论引用还是自研都要做**）。

### 自研（参考实现）——只对「小而带安全瑕疵」的划算

| 插件 | 建议 | 理由 |
| --- | --- | --- |
| `dsh-plugin-diff-review` | **vendor + 锁 `v0.1.2` tag + 设 `allowedRoots` 限定工作区** | 体量太大（≈380KB TS）重写不划算；作者可信；需收紧安全 |
| `dsh-subagent-max` | **先 vendor（或 npm 锁精确版本）；host 工具如要自研很便宜（≈200 行薄封装）** | 薄封装、无测试、作者一般；npm 已发布是加分项 |
| `dsh-file-changes` | **自研（或 vendor 后必须 patch reveal）** | 最小（≈24KB）、有安全瑕疵、作者信任最低 |

---

## 四、预装机制（两条路共同需要的新工作）

1. **启动 bootstrap**：把预装插件源码打进桌面 App（Go `embed`），dsh 就绪后写入 profile 的 `node_modules/` + `cordis.patch.yml`，或调用 `dsh plugin --profile web add`。需幂等（已装不重复）。
2. **收紧远程代理**：在 `remote.go` 按路径过滤插件 `/api/*`（至少 `file-changes/reveal`、`diff-review/files`、`apply`、`apply-hunk`、`commit`、`push`、`review`），或强制这些路由仅 loopback 可达，不让它们随 LAN 代理暴露。

---

## 五、一句话结论

- **不要盲引**；**diff-review 走 vendor + 锁 tag + allowedRoots**（别自研，太重）；**file-changes 建议自研**（最小且需修 reveal 安全）；**subagent-max 先 vendor/npm 锁版本**（host 工具后续可低成本自研）。
- 无论哪条路，都**必须先补上预装 bootstrap + 远程代理的插件 API 路径过滤**，否则预装即放大攻击面。
