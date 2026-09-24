## Agent skills

### Issue tracker

Issues and specs live as local markdown files under `.scratch/<feature>/` — never pushed to GitHub. See `docs/agents/issue-tracker.md`.

### Triage labels

Five canonical triage roles recorded as a `Status:` line in each issue file: `needs-triage` / `needs-info` / `ready-for-agent` / `ready-for-human` / `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.

## dsh 版本适配

用户要求「适配最新版本」时，改 `dsh.go` 的 `dshPackage` pin **之前**，必须先做一次**预装插件审计**（用户明确要求，每次都做）：

1. 对照新版本 release notes 与上游包，逐个核对 `preinstall.go` 的 `preinstalledPlugins`：是否有上游原生能力已取代它。
2. 被取代 / 不再需要的，移入 `retiredPlugins`（带确切的 `cordis.patch.yml` 卸载块），并同步 `README.md` / `README.en.md` 的预装插件列表。
3. 保留的插件，检查其对 `@deepseek-ai/dsh-*` 的 peer 范围是否覆盖新版本（semver 预发布元组规则：跨 minor 的 rc 不满足 `^0.1.5-rc.1`），不覆盖就更新并重建。
4. 把结论（保留 / 退役 / 新增）写进 `agent_context.md` 的「适配新 DSH 版本」一节。
