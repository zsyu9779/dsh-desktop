# DSH 插件调研报告

> 调研分支：`feature/plugin-research`（由 `main` 切出的独立 worktree，未影响 `feature/phone-remote` 上的进行中任务）
> 调研日期：2026-08-17
> 信息来源：各仓库 README / package.json / `docs/codex-gap-analysis.md` / npm registry / GitHub API

---

## 结论速览

| 需求 | 最匹配插件 | 覆盖情况 | npm 发布 | 额外依赖 |
| --- | --- | --- | --- | --- |
| Subagent 完整详情 | `@aaravarr/dsh-subagent-max` | ✅ 完全覆盖（多窗口实时 + 任务/reasoning/工具/输出/模型/token/step/上下文占用） | ✅ 有（latest 0.1.1） | 无 |
| 每轮文件改动预览 | `dsh-file-changes` | ✅ 完全覆盖（回复下方列表 + diff 弹窗 + 打开/定位） | ❌ 需从 GitHub 装 | 无 |
| Codex 式完整 Review | `dsh-plugin-diff-review` | ✅ 完全覆盖且大幅超出（工作台/单双栏 diff/文件树/图片/暂存丢弃提交/行评论 + AI 评审/PR/多仓库/编辑器联动） | ❌ 需从 GitHub 装 | `dsh-plugin-open-editor` |

三款插件均为 **MIT** 协议，均为 **web platform** 的 DSH 插件。除 subagent-max 外，其余两款尚未发布到 npm，需用 `dsh plugin add git+…` / `github:…` 方式安装。

---

## 1. Subagent 完整详情 —— `aaravarr/dsh-subagent-max`

- **仓库**：https://github.com/aaravarr/dsh-subagent-max
- **npm 包**：`@aaravarr/dsh-subagent-max`（版本 0.1.0 / 0.1.1，latest = 0.1.1）
- **License**：MIT ｜ **Stars**：4 ｜ **Forks**：0 ｜ **最后更新**：2026-08-15
- **定位**："a `subagent_with_model` tool plus a live multi-panel subagent viewer"

### 形态（two-face 插件）
- **Host face**（`lib/index.js`）：注册 `subagent_with_model` 工具——对 `ctx.subagents` 的薄封装，把 `model`/`provider` 透传到子代理的 `agentOptions`。
- **Client face**（`lib/client.js`）：Web UI，把每个 subagent 渲染为可拖拽、可缩放、实时流式输出的浮动面板，外加一个 **Subagents** 页签的卡片网格。

### 需求逐项覆盖

| 需求点 | 覆盖 | 说明 |
| --- | --- | --- |
| 多窗口 | ✅ | 可同时打开多个 subagent 面板 |
| 实时展示 | ✅ | 逐 token 实时流式输出 |
| 任务（task） | ✅ | prompt block 展示 |
| reasoning | ✅ | think 块单独渲染 |
| 工具调用 | ✅ | tool-call 卡片（含 input/output） |
| 输出 | ✅ | 文本流式输出 |
| 模型（model） | ⚠️✅ | 卡片网格显示模型；但派生自会话 `request/header`，个别子代理可能缺失 |
| token | ✅ | Subagents 页签卡片显示 |
| step | ✅ | Subagents 页签卡片显示 |
| 上下文占用 | ✅ | 卡片显示 context % |

### 额外能力
- **每调用指定模型/provider**：`subagent_with_model` 工具，可显式给子代理选模型（如 `deepseek-v4-flash`）。
- **拖出弹出**：把卡片拖到画布上，面板在你落点位置打开（带 ghost 预览）。
- **通知**：子代理启动 / 收到消息时侧上方 toast。
- **i18n**：zh / en，可在 DSH 设置切换。

### 安装与配置
```sh
dsh plugin --profile web add @aaravarr/dsh-subagent-max
```
```yaml
# cordis.patch.yml
- insert:
    - id: dsh-subagent-max
      name: '@aaravarr/dsh-subagent-max'
      config:
        subagentProvider: spawn        # spawn | fork | acp
        toolName: subagent_with_model
        backgroundMode: continuable    # one-shot | continuable
        maxDepth: 3
```

### 已知限制
- 模型展示派生自会话 `request/header`，个别子代理可能拿不到。
- last-activity 时间在客户端跟踪并缓存在 `localStorage`，新加载后首次打开可能回退到会话 `updatedAt`。
- 客户端 UI 仅面向 web 平台。

---

## 2. 每轮文件改动预览 —— `mixin-ai/dsh-file-changes`

- **仓库**：https://github.com/mixin-ai/dsh-file-changes
- **npm**：❌ 未发布（从 GitHub 安装）
- **License**：MIT ｜ **Stars**：0 ｜ **Forks**：0 ｜ **最后更新**：2026-08-14
- **版本**：0.1.0 ｜ **engines**：node `^22.19.0 || >=24.0.0`
- **定位**："per-turn file-change panel with diff viewing and filesystem reveal"

### 功能
每次回复末尾显示 **file-change 面板**，当一轮会话创建/修改文件时，在收尾消息下列出每个文件，并附带三个动作：

| 动作 | 说明 |
| --- | --- |
| **Open** | 用系统默认应用打开文件 |
| **View diff** | 弹窗渲染本轮**实际应用的改动**（新文件显示整文件内容），基于 harness 的 DiffBlock 原语 |
| **Reveal** | 在系统文件管理器中定位并选中文件（macOS `open -R`、Windows `explorer /select,`、Linux 打开所在目录）；仅在 loopback 访问且有本地 opener 时出现 |

面板是对内置 "Produced" 行的补充：新增 created/modified 徽标、逐文件 diff、逐文件系统定位。

### 需求逐项覆盖

| 需求点 | 覆盖 | 说明 |
| --- | --- | --- |
| 每次回复下方列出改动文件 | ✅ | 每轮回答末尾渲染面板，created/modified 徽标 |
| 弹窗渲染实际 diff | ✅ | modal 渲染本轮真实改动（新文件为整文件内容） |
| 可打开/定位文件 | ✅ | Open（打开）+ Reveal（文件管理器定位） |

### 实现机制（双模式）
- **Native 模式**：客户端半区注册 conversation definition，从 presentation views（`card: 'diff'` hunks 或 `kind: 'edit'`）累积成功的 mutation 工具调用，经 `conversation.chat.turnTail` slot 链渲染。
- **Code Mode（`run_code`）**：嵌套 dispatch 在 wire 上不携带 presentation views，故 host 半区监听 `tools/execute` / `tools/result`（带 `parent` 的子调用），从 `write`/`edit`/`str_replace_editor` 参数重建改动记录，经 `/api/file-changes/changes` 提供；面板合并两路来源并按键去重。

### 安装
```sh
# 从 GitHub
dsh plugin --profile web add git+https://github.com/mixin-ai/dsh-file-changes
# 本地 checkout
dsh plugin --profile web add link:/path/to/dsh-file-changes
```

### 已知限制
- 终端命令（如 `bash`）间接创建的文件检测不到——与官方 produced-files 行同等的词表限制。
- host 侧 Code Mode 记录存内存（2 小时 / 每会话 2000 条环形缓冲），服务重启即清空。
- reveal 端点仅信任 loopback 同源 JSON 请求。

---

## 3. Codex 式完整 Review —— `Civitasv/dsh-plugin-diff-review`

- **仓库**：https://github.com/Civitasv/dsh-plugin-diff-review
- **npm**：❌ 未发布（从 GitHub 安装）
- **License**：MIT ｜ **Stars**：6 ｜ **Forks**：1 ｜ **最后更新**：2026-08-16
- **Tags**：v0.1.0 / v0.1.1 / v0.1.2（README 引用 `v0.1.2`；package.json 仍为 0.1.0）
- **依赖**：`dsh-plugin-open-editor`（Civitasv，需一并安装）
- **技术栈**：TypeScript + esbuild，含 14+ 个独立测试脚本，工程最重、最成熟
- **定位**："Codex-style diff review inside DeepSeek Harness"

点击会话页头 **变动** 打开右侧评审工作台。

### 需求逐项覆盖

| 需求点 | 覆盖 | 说明 |
| --- | --- | --- |
| 右侧变更工作台 | ✅ | 会话页头「变动」按钮打开右侧评审面板 |
| 单栏/双栏 diff | ✅ | 标题栏切换单栏/双栏，可跳转文件、收起 diff、显示/隐藏文件树 |
| 文件树 | ✅ | 可搜索、拖拽调宽、展开目录、定位当前文件；右键打开/复制路径/加入对话 |
| 图片预览 | ✅ | 文件页签支持图片预览 + 常见代码/文档格式高亮 |
| 暂存/丢弃/提交 | ✅ | file / hunk / all 三级：暂存、取消暂存、丢弃；提交/提交并推送/推送 |
| 行评论 | ✅ | diff 行旁加评论，集中确认后发给代理；处理后回复下方显示评审结果卡 + 改动摘要卡 |

### 超出需求的完整能力（据 `docs/codex-gap-analysis.md` v2）
- **AI 评审**（`/review`）：`POST /diff-review/review` + `ctx.llm.stream`，会话模型 / `reviewModel` 双来源，输出 P0–P3 发现 + 结论，评审→修复→复评审闭环。
- **评审范围切换**：最后一轮 / 未暂存 / 已暂存 / 提交 revision / 分支（对齐 Codex App 的 scope）。
- **PR（gh）集成**：`gh pr view` + `gh api pulls/{n}/comments`，PR 评论跳转到对应文件行、聚合发送；无 gh 时静默降级。
- **多仓库**：检测 cwd 及一层子目录的 git 仓库，客户端仓库选择器。
- **编辑器联动**：扩展 `dsh-plugin-open-editor` 支持 `file:line`，diff 头/行 hover 打开。
- **review_model / 评审准则**：`reviewProvider`/`reviewModel` 配置 + 内置评审准则。
- **AI 修复闭环**：评论/发现/PR 评论 → `session.prompt` 注入代理（失败退化为复制文本）。
- **文件页签编辑**：文本文件可直接编辑并自动保存（CodeMirror）。
- **历史时间线**：`git log` 提交时间线（标注本地/远程未推送）+ 提交 diff。
- **自动刷新**：切换工作区自动刷新 + 每 15s 静默刷新。
- **引用助手回复**：选中回复文字浮出「添加到对话」。

### 安装
依赖 `dsh-plugin-open-editor`，需一并安装并在 web profile 补丁中注册：
```sh
dsh plugin --profile web add github:Civitasv/dsh-plugin-open-editor#main
dsh plugin --profile web add github:Civitasv/dsh-plugin-diff-review#v0.1.2
```
```yaml
# ~/.dsh/profiles/web/cordis.patch.yml
- insert:
    - id: open-editor
      name: dsh-plugin-open-editor
    - id: diff-review
      name: dsh-plugin-diff-review
```
重启 DSH 生效。本地开发可用 `bash install.sh`（macOS/Linux）或 `install.ps1`（Windows）。

### 已知限制 / 注意
- 「最后一轮」范围不包含终端命令直接改的文件，需切到未暂存/已暂存范围看 Git 改动。
- 在编辑器打开失败时，确认 `dsh-plugin-open-editor` 已装并启用、目标编辑器可从 PATH 启动。
- README 指向 tag `v0.1.2`，但仓库 `package.json` 版本仍为 0.1.0——按 tag 安装即可。

---

## 安装方式汇总

| 插件 | 安装命令 | 需手动改 cordis.patch.yml？ |
| --- | --- | --- |
| subagent-max | `dsh plugin --profile web add @aaravarr/dsh-subagent-max` | 可选（要配置项才加） |
| file-changes | `dsh plugin --profile web add git+https://github.com/mixin-ai/dsh-file-changes` | 一般不需要 |
| diff-review | `dsh plugin --profile web add github:Civitasv/dsh-plugin-open-editor#main` + `… diff-review#v0.1.2` | ✅ 需要（两个 id 都要登记） |

---

## 风险与建议

1. **成熟度**：`dsh-plugin-diff-review` 最重也最成熟（TS + 大量测试 + 详细 gap 分析 + 6 stars）；`dsh-file-changes` 最轻、stars 0，但功能面窄、实现直白；`dsh-subagent-max` 介于两者之间（npm 已发布是加分项）。
2. **发布渠道**：仅 subagent-max 走 npm；另外两个靠 GitHub 安装，需注意 tag 固定（diff-review 用 `#v0.1.2`）。
3. **依赖**：diff-review 强依赖 `dsh-plugin-open-editor`，安装顺序与补丁登记不可省。
4. **peerDependencies**：三款插件 peer 依赖均对齐 `@deepseek-ai/* ^0.1.0-rc.6` 与 `@deepseek-ai/cordis ^4.0.1`，与本仓库使用的 DSH 运行时版本需保持一致（安装前可核对当前 profile 已装版本）。
5. **共性限制**：三者都无法捕获「终端命令间接改动的文件」（DSH 官方 produced-files 行同源限制）；diff-review 通过 Git 范围规避，file-changes 则明确列为 limitation。
