# 预装插件设计文档

> 分支：`feature/plugin-research`（worktree，基于 `main`）
> 关联：`docs/plugin-research.md`（插件调研）、`docs/plugin-preinstall-decision.md`（引用 vs 自研决策）
> 状态：已实现（P0–P3 落地）

实现说明：预装机制走「扁平 node_modules + cordis.patch.yml insert」路径（D1），插件运行时产物经 `go:embed` 打进二进制（D2）；四款插件当前均 **vendor 上游 + 锁版本**（`plugins/`），file-changes 的 reveal 安全由 remote 代理拒绝表兜底（P0）；「自研为主」的重写是后续跟踪项，未在本轮完成。

---

## 1. 概述

### 1.1 目标

让 dsh-desktop 桌面壳把三款功能性插件（subagent-max / file-changes / diff-review，外加其前置依赖 open-editor）作为**预装**：用户装好 App 后无需手动 `dsh plugin add` 即可使用。

四条硬约束：

1. **不破坏用户已有 profile** —— 只做追加、绝不覆盖用户手写的 `cordis.patch.yml` 或 `package.json`。
2. **幂等** —— 重复启动不重复安装、不产生脏状态。
3. **可升级 / 可卸载 / 可回滚** —— 版本化、带标记、失败可还原。
4. **不放大攻击面** —— 预装引入的插件 HTTP 路由不得通过 LAN 远程代理暴露。

### 1.2 非目标

- 不重写 DSH 的插件系统，沿用其 cordis 加载机制。
- 不修改上游 DSH Web UI 本体（与仓库 "Everything is still a plugin" 定位一致）。
- 「引用 vs 自研」的取舍不在本文展开，见 `plugin-preinstall-decision.md`；本文只覆盖**预装机制 + 远程代理安全**（两条路共同的前置工作）。

---

## 2. 背景：DSH 插件加载机制（已验证事实）

以下事实来自对钉住版本 `@deepseek-ai/dsh@0.1.0-rc.6` 源码（`~/.dsh-desktop/npm-cache-v1/_npx/*/node_modules/@deepseek-ai/`）与本地 `~/.dsh` 的实测：

1. **profile 目录**：`$DSH_HOME/profiles/`（`DSH_HOME` 未设时默认 `~/.dsh`）。桌面壳 `augmentedEnv()` 透传环境，因此子进程用的 `DSH_HOME` = `os.Getenv("DSH_HOME")`，为空则 `~/.dsh`。
2. **web profile**：`profiles/web/` 内含
   - `package.json`（`dsh.profile.bundles: ["@deepseek-ai/dsh-base", "@deepseek-ai/dsh-web-app"]`）
   - `cordis.patch.yml`（用户补丁层，顶层 YAML 数组：insert 列表 / disable / config 覆盖）
   - `cordis.yml`（生成物，勿改）
   - `pnpm-workspace.yaml`（`packages: [.]`, `nodeLinker: hoisted`, `autoInstallPeers: false`）
3. **扁平模块兜底**：`profiles/node_modules/` 是安装闭包的符号链接兜底（`healProfilesModuleFallback`），Node 从 profile 向上查找会命中它。
4. **两种插件形态**：
   - **bundle 插件**（package.json 声明 `dsh.bundle.patch`，自带 `cordis.patch.yml`）：`dsh plugin add` = `pnpm <args>` + 把包名 reconcile 进 `dsh.profile.bundles`。subagent-max、file-changes 属于此类。
   - **非 bundle 插件**（无 `dsh.bundle.patch`）：必须在 `cordis.patch.yml` 里手动 `insert`。diff-review（以及 open-editor）属于此类。
5. **`dsh plugin` 命令是 pnpm 转发器**：`spawnSync("pnpm", args, {cwd: profileDir})` + reconcile。git 来源插件还需在 `pnpm-workspace.yaml` 加 `allowBuilds`。
6. **关键结论**：`healProfilesModuleFallback` 只维护「dsh 安装依赖闭包」的符号链接，**不会删除/覆盖它不认识的外来 symlink**。因此可以绕开 pnpm，直接：**把插件包 symlink 进 `profiles/node_modules/<真实包名>` + 在 `profiles/web/cordis.patch.yml` 幂等追加 `insert` 块** —— 这正是 diff-review 官方 `install.sh` 的做法，对三类插件（bundle / 非 bundle）**统一生效**，且 host 半区（`name` 解析）与 client 半区（`exports["./client"]` + `dsh.client`）都覆盖。

---

## 3. 总体方案

采用三条腿：

1. **Vendor**：把 4 个插件包的**运行时产物**打进 App 二进制（`go:embed`），首次启动解压到 `~/.dsh-desktop/plugins/<name>@<version>/`。
2. **激活**：symlink 到 `profiles/node_modules/<name>` + 幂等 append `cordis.patch.yml`（走第 2.6 条「扁平兜底 + patch」路径，不依赖 pnpm）。
3. **安全**：`remote.go` 的认证中间件加路径拒绝表，阻断预装插件引入的 HTTP 路由经 LAN 代理外泄。

```
┌──────────────── dsh-desktop (Go/Wails) ────────────────┐
│  main.go ── startup() ──> preinstall (幂等)             │
│      │                       │                          │
│      │   go:embed plugins/** │                          │
│      ▼                       ▼                          │
│  dsh.go 启动 dsh web      ~/.dsh-desktop/plugins/<pkg>  │
│      │                       │ symlink                  │
│      └── remote.go (LAN)     ▼                          │
│           │ deny-list    ~/.dsh/profiles/node_modules/  │
│           ▼              ~/.dsh/profiles/web/cordis.patch.yml
│      手机 <─ 403 ─ /diff-review/* , /api/file-changes/*
└─────────────────────────────────────────────────────────┘
```

---

## 4. 模块设计

### 4.1 插件 Vendor 与清单

仓库新增 `plugins/` 目录，每个插件一个子目录，只保留运行时文件（不带 devDependencies / 测试 / 文档）：

```
plugins/
  manifest.json                  # 预装清单（版本 + 来源 + sha256 + 激活 insert）
  aaravarr-dsh-subagent-max@0.1.1/
    package.json  lib/index.js  lib/client.js  cordis.patch.yml
  mixin-ai-dsh-file-changes@0.1.0/
    package.json  lib/index.js  lib/client.js  cordis.patch.yml
  civitasv-dsh-plugin-diff-review@0.1.2/
    package.json  dist/index.js  client.js
  civitasv-dsh-plugin-open-editor@<tag>/
    package.json  dist/...  (或按实际产物)
```

`manifest.json` 结构（草案）：

```json
{
  "schemaVersion": 1,
  "dshPin": "@deepseek-ai/dsh@0.1.0-rc.6",
  "plugins": [
    {
      "id": "dsh-subagent-max",
      "name": "@aaravarr/dsh-subagent-max",
      "version": "0.1.1",
      "source": "https://github.com/aaravarr/dsh-subagent-max",
      "sourceRef": "<commit/tag 哈希>",
      "dir": "aaravarr-dsh-subagent-max@0.1.1",
      "insert": {
        "id": "dsh-subagent-max",
        "name": "@aaravarr/dsh-subagent-max",
        "config": {
          "subagentProvider": "spawn",
          "toolName": "subagent_with_model",
          "backgroundMode": "continuable",
          "maxDepth": 3
        }
      }
    },
    { "id": "file-changes", "name": "dsh-file-changes", "version": "0.1.0", "...": "..." },
    { "id": "open-editor",   "name": "dsh-plugin-open-editor",  "...": "..." },
    { "id": "diff-review",   "name": "dsh-plugin-diff-review",  "config": { "allowedRoots": ["<工作区>"] }, "...": "..." }
  ]
}
```

要点：

- `version` 与 `sourceRef` 双向锁定；vendor 目录名带版本，升级 = 新增目录 + 改清单，旧目录保留用于回滚。
- diff-review 的 `insert.config.allowedRoots` 建议**默认收紧到工作区**（见 4.3 / 决策 D3），至少不作为空数组直接预装。
- `sha256` 对每个 vendor 文件做完整性校验，防打包/解压损坏。

### 4.2 预装 Bootstrap（新增 `preinstall.go`）

**触发时机**：`app.startup()` 内、`dsh.start()` 之前同步执行（快、幂等；dsh 启动本身很慢，不抢时间片）。

**流程**（全部幂等）：

1. **解析 profile 目录**：`DSH_HOME = os.Getenv("DSH_HOME")`，为空则 `~/.dsh`；`profileDir = <DSH_HOME>/profiles`。
2. **版本 marker 检查**：读 `~/.dsh-desktop/preinstall-state.json`，若 `dshPin` + 各插件 `name@version` 与 manifest 一致 → 直接返回（跳过）。
3. **解压 vendor**：从 `go:embed` 把各插件目录写入 `~/.dsh-desktop/plugins/<dir>/`，校验 sha256。写临时目录 + 原子 rename，避免半成品。
4. **symlink**：对每个插件，`ln -sfn <pluginsDir>/<dir> <profileDir>/node_modules/<name>`（包名含 `@scope/` 时逐级 `mkdir -p` 父目录）。仅当目标是 symlink 或缺位时覆盖；若存在**真实目录**（用户自己 pnpm 装过同名包）→ 跳过并告警，不覆盖用户安装。
5. **patch 追加**：对 `profileDir/web/cordis.patch.yml` 做**幂等 append**：
   - 若已存在 `id: <id>` 行 → 跳过（不重复、不挪动用户内容）。
   - 否则在文件末尾追加带注释标记的 insert 块：
     ```yaml
     # dsh-desktop 预装（版本 <version>，由 dsh-desktop 管理，删除此行以上到下一个标记之间可卸载）
     - insert:
         - id: <id>
           name: <name>
           config: {...}   # 仅当有
     ```
   - 追加前备份原文件为 `cordis.patch.yml.bak-dsh-desktop-<ts>`。
6. **写 marker**：`preinstall-state.json` 记录 `{dshPin, plugins: [{id, name, version}]}` + 完成时间。

**失败处理**：任一步失败 → 回滚本次改动（还原 patch 备份、删除本次新建的 symlink/目录），记日志，**不阻塞 dsh 启动**（预装失败只降级为"未预装"，不影响壳的可用性）。用状态 `preinstall: ok | skipped | degraded` 透传到前端 `Status()`/日志。

**边界情况**：

- `web` profile 尚不存在（首次使用）→ 由 dsh 自身 `initProfile` 惰性创建；preinstall 发现不存在时，先 `mkdir -p` 并写入最小 `package.json` + 空 `cordis.patch.yml`（与 `initProfile` 同构），或退而只写 `~/.dsh-desktop/preinstall-state.json` 记录 pending，等 dsh 首启创建 profile 后**下一次启动**再补装（推荐后者，避免与 dsh 的 init 竞态）。
- 用户已手动安装同名插件（真实目录 / 已有同 id）→ 尊重用户，跳过，不覆盖。

### 4.3 远程代理安全（改 `remote.go`）

**问题**：`remoteManager` 是整站反向代理，把**所有** `/api/*`（以及 `/diff-review/*`）透传到 loopback，并把 `Origin` 改写成 loopback（`targetOrigin`），导致插件的 loopback-origin 校验被抵消；唯一门禁是配对 token。

**改动**：在 `authMiddleware` 里、`next.ServeHTTP` 之前加路径拒绝表：

```go
// remoteDenyPrefixes 是启用远程时禁止经 LAN 代理转发的路径前缀。
var remoteDenyPrefixes = []string{
    "/diff-review/",       // git 操作 / 文件读写 / AI 评审（diff-review）
    "/api/file-changes/",  // reveal（任意绝对路径）+ changes（file-changes）
}

func (r *remoteManager) denyRemote(path string) bool {
    for _, p := range remoteDenyPrefixes {
        if strings.HasPrefix(path, p) {
            return true
        }
    }
    return false
}
```

在 `authMiddleware` 通过鉴权后、转发前：

```go
if r.denyRemote(req.URL.Path) {
    http.Error(w, "not available over remote", http.StatusForbidden)
    return
}
```

**拒绝面**（两前缀全拒，最简单且安全）：

- `/diff-review/*`：`status/apply/apply-hunk/commit/push/history/commit-diff/comments/branches/review/pr/repos/files` —— 含 `git add/restore/commit/push`、文件读（≤1MB 文本）/写、AI 评审（烧 token）。手机端不需要 diff 审查，全拒。
- `/api/file-changes/*`：`reveal`（`open -R <任意绝对路径>`）+ `changes`。全拒。

**结论**：预装插件在桌面 loopback 上照常工作；启用 LAN 远程时，这些路由对手机一律 403。未来若要放开只读路由（如 `history`），把拒绝表改成显式 allow-list 即可，本文不做。

### 4.4 升级 / 卸载 / 回滚

- **升级**：改 `manifest.json`（新 version + sourceRef）+ vendor 新目录 + 重建二进制。preinstall 检测 marker 版本不一致 → 替换 symlink 目标 + 更新 patch 块（按 `id` 定位旧块替换 config）。旧 vendor 目录保留一个版本用于回滚。
- **卸载**：提供 `App.UninstallPreinstalledPlugin(id)`（或本地 CLI 子命令）——按标记删除对应 patch 块 + 删除 symlink（仅当 symlink 目标指向 `~/.dsh-desktop/plugins/` 时）+ 更新 marker。用户手动装的同名插件永不触碰。
- **回滚**：preinstall 每步都有备份（patch 备份文件）；marker 保留上次成功状态，失败自动还原。

---

## 5. 关键决策记录（取舍）

| # | 决策 | 备选 | 结论 |
| --- | --- | --- | --- |
| D1 | **激活路径** | A: `dsh plugin add`（bundles + pnpm）；B: 扁平 node_modules symlink + `cordis.patch.yml` insert | **选 B**：无需 pnpm、无需网络、三类插件统一处理、幂等可控、与 diff-review 官方 install.sh 一致 |
| D2 | **交付方式** | A: 首启网络拉取（git/npm）；B: `go:embed` 打进二进制 | **选 B**：离线可用、版本锁定、不依赖上游仓库存活；代价是二进制体积略增 |
| D3 | **diff-review `allowedRoots`** | A: 空（任意 cwd）；B: 预装时限定工作区 | **选 B**：预装默认收紧，用户可自行改 config 放开 |
| D4 | **profile 作用域** | A: 共享 `~/.dsh`；B: 桌面独享 `DSH_HOME` | **选 A + append-only**：与现状（桌面用共享 profile）一致，不割裂用户会话/设置；靠「只追加、带标记、备份」保证安全 |
| D5 | **远程代理** | A: 全透传；B: 路径拒绝表 | **选 B**：预装即需堵住 `/diff-review/*`、`/api/file-changes/*` |
| D6 | **插件来源** | A: 全部 vendor；B: 自研为主 + 大件 vendor；C: 先搭机制再定 | **选 B**（用户已裁决）：file-changes/subagent-max 自研、diff-review 这类大件 vendor 锁 commit |

---

## 6. 涉及文件清单

**新增**

| 文件 | 作用 |
| --- | --- |
| `plugins/manifest.json` | 预装清单（版本/sourceRef/sha256/insert） |
| `plugins/<name>@<version>/…` | vendor 的插件运行时产物（`go:embed` 源） |
| `preinstall.go` | 预装 bootstrap（解压/symlink/patch/marker/回滚） |
| `preinstall_test.go` | 幂等、跳过用户安装、失败回滚、manifest 解析测试 |

**修改**

| 文件 | 改动 |
| --- | --- |
| `main.go` | `go:embed all:plugins` + 注入 preinstaller |
| `app.go` | `startup()` 里 `preinstall()` 先行；新增 `UninstallPreinstalledPlugin` 绑定；`Status()` 透出 preinstall 状态 |
| `remote.go` | `authMiddleware` 加 `denyRemote` 路径拒绝 |
| `dsh.go` | （可选）把 preinstall 结果写进日志；`DSH_HOME` 解析复用 |

---

## 7. 分阶段实施计划

1. **P0 — 骨架与安全（先做，独立可合）**：`remote.go` 路径拒绝表 + 测试（这是纯加固，不依赖插件 vendor，先落地）。
2. **P1 — vendor + 预装 bootstrap**：`plugins/` 目录 + `manifest.json` + `preinstall.go` + 单测；先只预装**一个**插件（建议 file-changes，最小）验证全链路。
3. **P2 — 扩到全部四款**：补 subagent-max / diff-review / open-editor，diff-review 默认 `allowedRoots`，做端到端冒烟（启动后确认按钮/面板出现、`cordis.yml` 正确合成、远程拒绝生效）。
4. **P3 — 升级/卸载/回滚 + 文档**：升级路径、卸载 API、回滚演练、更新 README 的「插件由上游管理」表述以反映预装事实。

---

## 8. 风险与回滚

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| 上游 dsh 升 rc 后插件 peer 依赖/注入服务名变化 | 预装插件加载失败 | manifest `dshPin` 与 dsh.go 的 `dshPackage` 联动升级；冒烟测试兜底；`assertEntriesActivated` 会 fail loud 可见 |
| 用户 profile 被污染 | 破坏用户环境 | append-only + 备份 + 同 id/同包跳过 + 卸载只删自己标记 |
| `healProfilesModuleFallback` 行为在版本间变化 | symlink 被管理 | 依赖其"只维护安装闭包、不删外来链接"契约；升级 dsh 时重跑冒烟 |
| 远程拒绝表误伤正常功能 | 手机少个功能 | 两前缀与手机主流程（聊天/文件浏览）无交集；误伤面极小，且可回退为 allow-list |
| vendor 增大二进制 | 体积 | 仅运行时产物、去 devDeps/文档/截图；可评估用 zip 压缩后 embed |

**回滚**：任一预装步骤失败 → 还原 patch 备份 + 删除本次新建 symlink/目录，dsh 照常启动（降级为"未预装"）；远程拒绝表独立，可单独 revert。

---

## 9. 验收标准

1. 全新环境首次启动 dsh-desktop：三款插件自动就绪（页头「变动」按钮出现、回复末尾文件改动面板出现、Subagents 页签出现），全程无手动 `dsh plugin add`。
2. 第二次启动：preinstall 直接跳过，无重复 append、无脏 symlink。
3. 用户已手动安装同名插件 / 已有同 id 时：不覆盖、不重复，日志有告警。
4. 启用 LAN 远程后，从手机访问 `/diff-review/*` 与 `/api/file-changes/*` 一律 403；桌面 loopback 上功能不受影响。
5. 卸载任一预装插件后：对应 patch 块与 symlink 消失，用户自己的插件不受影响，dsh 可正常重启。
6. `go test ./...` 通过（含 preinstall 幂等/回滚、remote deny 单测）。
