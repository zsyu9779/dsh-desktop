# dsh-agent-preset-compat

让 **DSH 0.1.1 时代创建的会话**在升级到 0.1.2+ 之后仍可继续。

## 它解决什么

DSH 0.1.2 把随包提供的 agent preset `code` 改名成了 `ptc`（显示名一直叫「PTC 模式」），
但每个会话记录的首行仍然写着旧 id：

```json
{"type":"session","id":"session-…","agentPreset":"code"}
```

于是升级后打开老会话（或默认 preset 设置仍是 `code` 时新建会话）会失败：

```
agent-presets: preset "code" not found (available: cordis, minimal, ptc, standard)
```

本插件在启动时把**替代 preset 的目录**复制到用户 preset 根下的旧 id 目录，
让老 id 重新可解析。它**不改动任何会话历史**。

## 行为

- 启动时读取当前名册：
  - 旧 id 已由某个可用的 preset 提供 → 跳过（例如回到 0.1.1，或官方重新提供旧 id）。
  - 旧 id 存在但不可挂载（broken）→ 告警并保持原样，**不覆盖**。
  - 替代 id 不存在 → 告警并跳过。
- 否则把**当前生效的**替代 preset 目录（`agent.cordis.yml` + `preset.yml` 及子目录）复制到
  `$DSH_HOME/.agent-presets/<旧 id>/`。
- 复制是**原子**的：先写同级的 `*-staging` 目录，再整体 rename 就位；中途失败只留下 staging，
  不会占住旧 id 变成一个永远 broken 的目录。
- 副本内写一个标记文件 `.dsh-agent-preset-compat.json`（记录来源 id 与 composition 的 SHA-256）。
  - 下次启动若来源 composition 的摘要变了（例如再升级到 0.1.5，`ptc` 组合有增改），
    本插件会**自动刷新**这份副本。
  - 目录里没有该标记 → 视为他人所写，**永不触碰**。
- 用户根在名册里排在最后，所以将来官方重新提供旧 id 时，会自动盖过这个别名。
- 幂等：内容未变时不会重写。

## 配置

```yaml
- insert:
    - id: agent-preset-compat
      name: 'dsh-agent-preset-compat'
      config:
        aliases:
          code: ptc
```

`aliases` 是「已退役 id → 替代 id」的映射，默认 `{ code: ptc }`；将来再有改名时改这里即可。

## 卸载

删掉插件行，并删除 `$DSH_HOME/.agent-presets/<旧 id>/` 即可完全还原。

## 备注

别名指向的是**新版**的 `ptc` 组合，而不是旧版组合的副本 —— 旧组合引用的插件在新版本里未必存在。
