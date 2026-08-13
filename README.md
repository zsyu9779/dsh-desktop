# DeepSeek Harness Desktop

非官方的 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) 桌面壳：用 [Wails](https://wails.io)（Go）包住官方 `@deepseek-ai/dsh` 的 Web UI，做成一个像 Codex 那样的原生桌面应用。

> ⚠️ 本项目与 DeepSeek / DeepSeek AI 无任何关联，是社区作品。DeepSeek Harness 本身仍处于开发者预览阶段（v0.1），接口可能随时变更。

## 它是怎么工作的

- 启动时在受管端口（优先 `3080`，被占用则随机）拉起 `npx -y @deepseek-ai/dsh@0.1.0-rc.6 web`
- 轮询本地 Web UI 直到就绪，然后把窗口重定向到该地址
- 退出应用时清理整个进程树（npx → node → dsh）；服务意外退出会弹原生对话框并退出

所有会话、模型、插件、设置均由上游 DeepSeek Harness 提供，本项目不修改、不重实现其 UI。

## 特性

- 原生窗口 / Dock 图标 / 应用菜单
- 启动 splash：状态、重试、在浏览器打开、查看日志
- 自动安装并固定 DeepSeek Harness 版本
- 日志写入 `~/.dsh-desktop/logs/dsh.log`

## 下载 / 安装

前往 [Releases](https://github.com/zsyu9779/dsh-desktop/releases) 下载对应平台的安装包：

- **macOS**：`dsh-desktop-darwin-universal.dmg`（Intel + Apple Silicon 通用）
- **Windows**：`dsh-desktop-windows-amd64-installer.exe`（安装向导），或同目录的 `.exe` 免安装版
- **Linux**：`dsh-desktop-linux-amd64.tar.gz`

> macOS 首次打开未签名应用会被 Gatekeeper 拦截：右键应用「打开」即可放行；或在终端执行
> `xattr -d com.apple.quarantine "/Applications/dsh-desktop.app"`。

## 环境要求

- Go 1.23+
- Node.js 18+（含 npx）
- Wails v2：`go install github.com/wailsapp/wails/v2/cmd/wails@latest`

## 构建

```bash
wails build
# 产物在 build/bin/dsh-desktop.app
```

## 开发

```bash
wails dev
```

## 发布新版本

推送 `v*` 标签即可触发 GitHub Actions 自动构建 macOS / Windows / Linux 三平台安装包并发布到 Releases：

```bash
git tag v0.1.0
git push origin v0.1.0
```

## 配置（环境变量）

| 变量 | 作用 |
| --- | --- |
| `DSH_COMMAND` | 覆盖启动命令，例如 `DSH_COMMAND="pnpm dsh"` 或本地源码里的 dsh 路径 |
| `DSH_WORKSPACE` | dsh 的工作目录（默认用户主目录） |
| `DSH_HOME` | 透传给 dsh，控制 profiles 存储位置 |

## 更新 DeepSeek Harness 版本

修改 `dsh.go` 中的 `dshPackage` 常量。

## 许可

[MIT](./LICENSE)。DeepSeek Harness 本身为 MIT 许可。
