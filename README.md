<p align="center">
  <img src="./assets/hero.jpg" alt="DeepSeek Harness Desktop：官网视觉背景上的原生应用窗口" width="100%">
</p>

<p align="center">
  中文 · <a href="./README.en.md">English</a>
</p>

# DeepSeek Harness Desktop

**Unofficial cross-platform desktop app for DeepSeek Harness — a native Wails shell for the DSH Web UI on macOS, Windows, and Linux.**

DeepSeek Harness Desktop 是一个开源、跨平台的 DeepSeek Harness 桌面客户端。它使用 Go 和 Wails 将官方 `@deepseek-ai/dsh` Web UI 封装为原生窗口，为 DSH 提供更直接的桌面启动、进程管理和日志查看体验。

[![Latest Release](https://img.shields.io/github/v/release/zsyu9779/dsh-desktop?display_name=tag)](https://github.com/zsyu9779/dsh-desktop/releases/latest)
[![Release](https://github.com/zsyu9779/dsh-desktop/actions/workflows/release.yml/badge.svg)](https://github.com/zsyu9779/dsh-desktop/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platforms](https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-lightgrey)](https://github.com/zsyu9779/dsh-desktop/releases/latest)

> [项目官网](https://zsyu9779.github.io/dsh-desktop/) · [下载最新版](https://github.com/zsyu9779/dsh-desktop/releases/latest) · [查看 DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) · [报告问题](https://github.com/zsyu9779/dsh-desktop/issues)

> [!IMPORTANT]
> 本项目是社区维护的非官方桌面壳，与 DeepSeek 或 DeepSeek AI 无隶属关系。DeepSeek Harness 仍处于开发者预览阶段，其功能和接口可能发生变化。

## 什么是 DeepSeek Harness Desktop？

DeepSeek Harness Desktop 是 DeepSeek Harness 的桌面启动器和原生容器，也可以理解为一个非官方 DSH GUI 客户端。它不重新实现 Harness，而是启动固定版本的官方 npm 包，并在桌面窗口中显示其 Web UI。

项目适合希望通过原生窗口使用 DeepSeek Harness、减少命令行启动步骤，或需要自动管理 DSH 后台进程的 macOS、Windows 和 Linux 用户。

## 核心功能

- **跨平台桌面应用**：支持 macOS、Windows 和 Linux。
- **原生应用体验**：提供独立窗口、Dock/任务栏图标和应用菜单。
- **自动启动 DSH**：通过 `npx` 拉取并运行固定版本的 `@deepseek-ai/dsh`。
- **可靠的进程管理**：关闭窗口或退出应用时清理 `npx → node → dsh` 进程树。
- **稳定的窗口恢复**：保留 Wails 原生容器，修复 macOS 最小化后恢复白屏的问题。
- **局域网手机远程**：同一 Wi-Fi 下扫码即可在手机浏览器操控当前 DeepSeek Harness。
- **启动状态与错误提示**：显示环境检查、首次下载、服务启动和失败状态。
- **日志与故障排查**：可在启动页查看日志，也会写入本地日志文件。
- **可配置工作目录**：支持通过环境变量覆盖命令、工作区和 DSH Home。

DeepSeek Harness 的对话、模型、插件、会话和设置能力均由上游项目提供。本项目只负责桌面容器、启动流程和子进程生命周期。

## 手机远程（局域网）

电脑和手机连在同一个可信 Wi-Fi 后，在启动面板点击「开启」，用手机扫描二维码即可打开当前 DeepSeek Harness。无需账号、无需云端中继，任务、会话和文件仍留在电脑上。

<p align="center">
  <img src="./assets/remote-control.png" alt="DeepSeek Harness Desktop 局域网手机扫码远程控制" width="82%">
</p>

- 通过带随机 token 的二维码完成配对，扫码后写入 HttpOnly Cookie
- 可以随时重新生成配对码，让此前的手机会话立即失效
- 关闭手机远程后，局域网代理和当前配对凭据会一并停止

> 当前版本是面向**可信局域网**的 HTTP 直连，不是公网远程访问。请勿在咖啡厅、公司访客网络等不可信 Wi-Fi 中开启。

## 下载与安装

运行 DeepSeek Harness Desktop 前，请先安装 [Node.js 18 或更高版本](https://nodejs.org/)。应用依赖 Node.js 附带的 `npx` 下载和启动 DSH。

前往 [GitHub Releases](https://github.com/zsyu9779/dsh-desktop/releases/latest) 下载对应平台的安装包：

| 平台 | 架构 | 发布文件 |
| --- | --- | --- |
| macOS | Intel + Apple Silicon | `dsh-desktop-darwin-universal.dmg` 或 `.zip` |
| Windows | amd64 | `dsh-desktop.exe` |
| Linux | amd64 | `dsh-desktop-linux-amd64.tar.gz` |

### macOS

下载并打开 DMG，将应用拖入 Applications。由于当前发布包未签名，首次启动时可右键应用并选择“打开”。

也可以在终端移除下载隔离标记：

```bash
xattr -d com.apple.quarantine "/Applications/dsh-desktop.app"
```

### Windows

下载 `dsh-desktop.exe` 后直接运行。若 Windows SmartScreen 提示未知发布者，请确认文件来自本仓库的官方 Release 页面。

### Linux

下载并解压 `dsh-desktop-linux-amd64.tar.gz`，然后运行其中的 `dsh-desktop` 可执行文件。桌面环境需要 WebKitGTK 等常规 Wails 运行依赖。

## 工作原理

应用启动后会完成以下流程：

1. 检查本机能否找到 Node.js 和 `npx`。
2. 在独立 npm 缓存中运行固定版本的 `@deepseek-ai/dsh`。
3. 优先使用本地端口 `3080`；端口被占用时自动选择可用端口。
4. 轮询 DSH Web 服务，等待其返回成功响应。
5. 在保留 Wails 顶层容器的前提下，通过全屏页面载入 DSH UI。
6. 窗口关闭或应用退出时，终止完整的 DSH 子进程树。

```text
DeepSeek Harness Desktop (Wails / Go)
└── npx @deepseek-ai/dsh web
    └── node / dsh
        └── Local Web UI on 127.0.0.1
```

应用默认固定上游版本 `@deepseek-ai/dsh@0.1.0-rc.6`，避免每次启动因上游最新版变化而产生不可预测的行为。

## 配置

| 环境变量 | 作用 |
| --- | --- |
| `DSH_COMMAND` | 覆盖启动命令，例如 `DSH_COMMAND="pnpm dsh"` 或本地 DSH 可执行文件路径 |
| `DSH_WORKSPACE` | 设置 DSH 工作目录；默认使用当前用户主目录 |
| `DSH_HOME` | 透传给 DSH，用于控制 profiles 等数据的位置 |

应用日志默认保存在：

```text
~/.dsh-desktop/logs/dsh.log
```

应用使用独立 npm 缓存，减少共享 `_npx` 缓存损坏或并发访问造成的 `sh: dsh: command not found` 问题。

## 从源码构建

开发环境需要：

- Go 1.23+
- Node.js 18+
- Wails v2.11+

安装 Wails CLI 并构建：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.11.0
wails build
```

macOS 构建产物位于：

```text
build/bin/dsh-desktop.app
```

启动开发模式：

```bash
wails dev
```

## 常见问题

### 这是 DeepSeek 官方项目吗？

不是。DeepSeek Harness Desktop 是社区维护的非官方开源项目。官方 Harness 源码位于 [`deepseek-ai/deepseek-harness`](https://github.com/deepseek-ai/deepseek-harness)。

### 它与 DeepSeek Harness Web UI 有什么区别？

界面和核心能力来自官方 DSH Web UI。本项目增加原生桌面窗口、自动启动、端口选择、日志展示、进程清理和跨平台发布包。

### 为什么必须安装 Node.js？

DeepSeek Harness 通过 npm 发布。桌面应用使用 Node.js 和 `npx` 下载并运行固定版本的 `@deepseek-ai/dsh`，因此当前版本不是完全独立的离线安装包。

### 关闭应用后 DSH 还会在后台运行吗？

正常关闭窗口或退出应用时，桌面壳会终止完整的 `npx → node → dsh` 进程树。若应用被操作系统强制终止，仍建议通过任务管理器确认是否存在异常残留进程。

### 数据和设置保存在哪里？

会话、模型、插件和设置由上游 DeepSeek Harness 管理。可通过 `DSH_HOME` 调整其 profiles 等数据目录；桌面壳自己的日志保存在 `~/.dsh-desktop/logs/`。

### 首次启动为什么比较慢？

首次运行需要通过 npm 下载 DeepSeek Harness 及其依赖，耗时取决于网络和 npm registry。完成缓存后，后续启动通常会更快。

### 出现白屏或启动失败怎么办？

先确认 `node --version` 和 `npx --version` 可用，再查看启动页日志或 `~/.dsh-desktop/logs/dsh.log`。提交 Issue 时请附上操作系统、应用版本和相关日志。

## 发布流程

推送 `v*` 标签会触发 GitHub Actions，为 macOS universal、Windows amd64 和 Linux amd64 构建产物，并创建 GitHub Release。

```bash
git tag v0.1.5
git push origin v0.1.5
```

更新 DeepSeek Harness 版本时，请修改 `dsh.go` 中的 `dshPackage` 常量并完成真实启动测试。

## 许可证

DeepSeek Harness Desktop 使用 [MIT License](LICENSE)。上游 DeepSeek Harness 也采用 MIT License，但两者是独立项目。

如果这个项目对你有帮助，欢迎提交 Issue、Pull Request 或 Star。
