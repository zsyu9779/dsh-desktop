# DSH Desktop Remote Control

远程操控桌面端 DeepSeek Harness 的语境：手机作为远程视图操控跑在主机上的 dsh。免费档是「扫码即用的本地配对」，订阅档叠加一个云端账号，提供身份、中继路由、推送与设备注册表。

## Language

**Account**:
A cloud identity a user signs in with, available only in the subscription tier. It binds the user's hosts and devices and provides identity, routing, push, and the device registry. It does not host session state.
_Avoid_: 用户, user, login, session

**Host**:
The user's computer where dsh runs and where sessions, files, and credentials live. It is the server; the phone is only a remote view, so control requires the host to be online.
_Avoid_: 桌面端, machine, server, desktop

**Device**:
A phone or tablet running the mobile app, used as a remote view onto a host's dsh.
_Avoid_: 手机, client, 移动端, mobile

**Pairing**:
Establishing a trusted, per-device link between a device and a host.
_Avoid_: 绑定, 连接, connect, bind

**Session**:
A conversation and working context on a host — the unit of history and continuation. This is what Codex calls a "thread".
_Avoid_: 线程, thread, 对话, chat

**Goal**:
A long-running objective being pursued within a session. This is what Codex calls a "task".
_Avoid_: 任务, task, mission

**Task**:
A discrete unit of work or job inside a session.
_Avoid_: 子任务, todo, job

**Agent**:
The coding-agent product being remotely controlled. In v1 this is dsh only; cross-agent control (Claude Code, Codex, …) is rejected, not merely deferred.
_Avoid_: 工具, tool, product, app

**Model provider**:
A model vendor selectable inside dsh (Claude, DeepSeek, GPT, …) — dsh's own feature, reused unchanged. Distinct from Agent.
_Avoid_: agent (a provider is not an Agent)

**Owner**:
The person who owns the Host and authorizes Pairings and scope grants; referred to as "Host 的所有者" in prose.
_Avoid_: 用户, user, 开发者
