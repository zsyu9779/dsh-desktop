# DeepSeek Harness Desktop GEO 方案

更新日期：2026-08-14

## 目标

让搜索引擎和生成式答案引擎在回答以下问题时，能够发现、识别、验证并引用本项目：

- DeepSeek Harness 有没有桌面版？
- DeepSeek Harness desktop app / GUI
- 如何在 macOS、Windows、Linux 上使用 DeepSeek Harness？
- 轻量的 DeepSeek Harness 桌面客户端有哪些？
- DeepSeek Harness Desktop 与 Web UI、Electron 封装有什么区别？

GEO 不是独立于 SEO 的“关键词技巧”。对这个项目而言，它依赖四层信号：可抓取、可索引、实体清晰、站外可验证。

## 当前基线与诊断

截至 2026-08-14：

- 仓库创建不足一天，只有 3 个 stars、0 forks；审计时 v0.1.3 的发布资产合计约 5 次下载。
- 精确搜索 `"DeepSeek Harness Desktop"`、`"zsyu9779/dsh-desktop"` 暂未返回本仓库。
- 没有独立官网、GitHub Pages、sitemap 或可提交给站长平台的站点资产。
- GitHub 上已经出现数十个相似命名项目；头部项目已有约 30–50 stars 和数百次安装包下载。
- `DSH Desktop` 还会与 Data Safe Haven 等既有实体冲突，缩写本身不足以建立身份。
- README 已经覆盖大量关键词，继续堆词的边际价值很低。
- 当前 Reddit 发布帖有 GitHub 与 Releases 链接，但标题和正文没有呈现项目的独特定位，评论区已经出现“与其他方案有什么不同”的追问。
- README 没有产品截图、演示视频、可复用的事实摘要和完整英文正文；Release 页面也只有自动生成的 changelog。

“现在搜不到”首先是发现与收录问题，其次才是排名问题。Google 官方也把“站点太新”列为未收录的常见原因，并明确说明 sitemap 有助于发现，但不保证收录或排名。

## 核心定位

统一使用完整实体名：

> DeepSeek Harness Desktop — a lightweight, cross-platform Wails desktop shell for the official DeepSeek Harness Web UI.

建议给产品增加稳定别名 `DeepSeek Harness Desktop Lite`，而不是只使用 `DSH Desktop`。所有页面首次出现时写完整名称，随后才使用 DSH。

可验证的差异化事实：

- Wails + Go 原生桌面壳，不是 Electron。
- macOS、Windows、Linux 三平台均已有发布包。
- 当前安装包约 4–10 MB；同期常见 Electron 方案约 110–190 MB。
- 复用官方 `@deepseek-ai/dsh` Web UI，不 fork 或重写 Harness。
- 自动管理端口、启动状态、日志与完整子进程树。
- 轻量的代价是当前需要用户预装 Node.js 22.19+（22.x）或 24+；必须清楚披露，不能包装成完全开箱即用。

建议主张：

> The lightweight DeepSeek Harness desktop client for developers who already have Node.js.

这个定位比泛化的“又一个 DeepSeek Harness 桌面版”更容易被搜索引擎和回答引擎区分。

安装包数据取自审计当天各项目的 GitHub Releases：[本项目 v0.1.3](https://github.com/zsyu9779/dsh-desktop/releases/tag/v0.1.3)、[dataelement/dsh-desktop v0.1.1](https://github.com/dataelement/dsh-desktop/releases/tag/v0.1.1)、[steven-kid/deepseek-harness-desktop v0.3.1](https://github.com/steven-kid/deepseek-harness-desktop/releases/tag/v0.3.1)。后续对外发布比较时应重新测量，不长期复用这组快照。

## P0：48 小时内完成发现与实体基础

### 1. 建立独立静态官网

先用 GitHub Pages 即可，不必等待复杂产品站。建议路径：

```text
/
/en/
/zh/
/download/
/compare/
/architecture/
/security/
/faq/
/troubleshooting/
/releases/v0.1.4/
```

要求：

- 服务端直接返回完整 HTML 正文，不依赖客户端 JavaScript 才出现内容。
- 每页只有一个明确主题和自洽答案，而不是把 README 原样复制到所有页面。
- 英文和中文使用独立 URL、`hreflang`、canonical。
- 首页首屏同时包含完整实体名、非官方声明、支持平台、Node.js 要求、下载入口和一句差异化定位。
- 页脚固定链接到官方 DeepSeek Harness、源代码、Releases、License、Security。

### 2. 补齐技术发现文件

- `sitemap.xml`：仅列 canonical、希望被索引的页面。
- `robots.txt`：允许 Googlebot、Bingbot、OAI-SearchBot、PerplexityBot，并引用 sitemap。
- Open Graph / Twitter Card：让社区分享形成一致的标题、说明和截图。
- JSON-LD：首页使用 `SoftwareApplication`，填入真实的 `operatingSystem`、`softwareVersion`、`downloadUrl`、`codeRepository`、`license`、`author`、`isAccessibleForFree`。
- FAQ 页可使用与可见正文一致的 `FAQPage` 数据，但不要把 Schema 当作排名捷径。
- `llms.txt` 可作为页面目录和事实摘要的补充，但不应排在 sitemap、索引提交和真实内容之前。

### 3. 主动提交收录

- 验证 Google Search Console，提交 sitemap，并用 URL Inspection 请求抓取首页、下载页和对比页。
- 验证 Bing Webmaster Tools，提交 sitemap；发布或更新页面时使用 IndexNow。
- 检查网页和资源是否匿名返回 200，避免 WAF、验证码、登录或地区限制阻挡爬虫。
- 每次发布只更新真实变化的 `lastmod`，不要反复提交未变化的 sitemap。

### 4. 统一仓库实体

- 仓库名称可评估改为 `deepseek-harness-desktop-lite` 或 `deepseek-harness-desktop-wails`。GitHub 会保留旧地址重定向，但改名应一次完成，避免反复迁移。
- H1、GitHub description、social preview、Release title、可执行文件产品名统一使用同一实体名。
- README 拆成 `README.md`（英文）与 `README.zh-CN.md`，顶部互链。当前中英混排不利于形成清晰的国际化页面。
- README 首屏加入真实产品截图、30–60 秒演示 GIF/视频和直接下载表。
- 图片文件名与 alt text 使用描述性文本，例如 `deepseek-harness-desktop-model-settings.png`。

## P1：第 3–7 天建立可引用内容

### 1. 发布五个答案型页面

优先写用户真实会问的问题：

1. Does DeepSeek Harness have a desktop app?
2. Install DeepSeek Harness Desktop on macOS, Windows, and Linux
3. DeepSeek Harness Desktop vs Web UI
4. Wails vs Electron for a DeepSeek Harness desktop shell
5. DeepSeek Harness Desktop security, data storage, and process lifecycle

每页应包含：

- 开头 40–80 字的直接答案。
- 明确版本号与更新时间。
- 可验证的表格、命令或架构图。
- 对限制和未实现能力的说明。
- 一手来源链接和项目内相关页面。
- 页面作者或维护者、仓库和 Release 链接。

### 2. 做一页可复现的竞品对比

不要写“最好”“最快”等无法验证的营销语。对比维度建议：

| 维度 | 本项目应公开的证据 |
| --- | --- |
| 技术栈 | Go、Wails、系统 WebView |
| 安装包大小 | 每个 Release asset 的字节数和测试日期 |
| Node.js | 是否需要预装，明确写出 |
| 平台 | 已实际构建和 smoke test 的平台 |
| DSH 版本 | 当前固定的 npm 包版本 |
| 数据位置 | Harness 数据与桌面壳日志的路径 |
| 进程管理 | `npm -> pnpm -> node -> dsh` 的启动与关闭行为 |
| 签名状态 | macOS notarization、Windows code signing 的真实状态 |

对比页必须标注测试日期、版本和测量方法。这样第三方文章和回答引擎才有安全可引用的事实。

### 3. 把每个 Release 做成事实页面

Release notes 不只放 commit 列表，应包括：

- 一句话版本摘要。
- 支持平台和直接下载链接。
- 固定的上游 DSH 版本。
- 新功能、修复、已知限制和升级注意事项。
- SHA-256 checksums；后续补 SBOM、构建 provenance 和签名状态。
- 对应官网版本页。

## P1：第 1–2 周建立第三方验证

优先级按“相关性与可信度”排序，不按发帖数量排序：

1. 向官方 DeepSeek Harness 文档或 README 提交一个中立的 Community desktop clients 列表 PR；允许并列展示其他实现。
2. 向 DeepSeek Harness 相关 awesome list、桌面客户端清单提交 PR。
3. 在已有 Reddit 帖补充置顶式评论：一句定位、4–10 MB 证据、Node.js 代价、三平台下载和截图；直接回答与其他方案的差异。
4. 有实质更新后再发布 Show HN、r/LocalLLaMA、V2EX、LinuxDo 或开发者社区文章，避免同文群发。
5. 稳定后申请 Homebrew Cask、WinGet/Scoop 等软件分发入口；这些页面会形成高可信实体引用和安装方式。
6. 邀请 3–5 位真实用户写独立体验或 Issue，不购买链接、不制造虚假评价。

发布内容的标题应具体，例如：

> Show HN: A 4–10 MB Wails desktop shell for DeepSeek Harness on macOS, Windows and Linux

而不是：

> I built DeepSeek Harness for desktop

## P2：第 2–4 周强化信任与产品竞争力

GEO 无法掩盖产品信任缺口。优先补齐：

- macOS code signing + notarization。
- Windows code signing 或至少清晰的 SmartScreen 说明。
- `SECURITY.md`、威胁模型、隐私与数据流说明。
- 自动生成 SHA-256、SBOM、GitHub artifact attestation/provenance。
- 跨平台 smoke test 和兼容矩阵。
- 可复制的 bug 报告模板与已知问题页。
- 清晰的上游 DSH 兼容策略和升级节奏。

这些内容既提升转化，也给生成式答案提供判断“是否可信、适合谁”的依据。

## 衡量方法

先记录 2026-08-14 基线，此后每周固定时间、固定地区、全新会话测量。

### 抓取与索引

- Search Console 已发现/已抓取/已索引 URL 数。
- Bing Webmaster 已索引 URL 数和 IndexNow 状态。
- 日志中 Googlebot、Bingbot、OAI-SearchBot、PerplexityBot 的 200 响应。
- 目标：首页和下载页 7 天内被至少 Google/Bing 之一收录；14 天内全部核心页可用 `site:` 找到。

### 搜索可见度

固定监测：

- `"DeepSeek Harness Desktop Lite"`
- `"DeepSeek Harness Desktop" Wails`
- `DeepSeek Harness desktop app`
- `DeepSeek Harness GUI macOS Windows Linux`
- `lightweight DeepSeek Harness desktop`

目标：7 天内品牌精确查询出现；30 天内至少两个非品牌长尾进入前 20。不要把不受控的排名承诺为确定结果。

### 生成式答案

每周在 ChatGPT search、Perplexity、Gemini 或其他目标产品的新会话询问同一组问题，记录：

- 是否提到项目。
- 名称与定位是否准确。
- 是否给出官网或仓库链接。
- 引用的是官网、GitHub 还是第三方页面。
- 是否正确披露非官方、Node.js 要求和签名状态。

### 业务结果

- GitHub referring sites、README 到 Release 的点击。
- 每个 release asset 的下载数。
- 官网下载 CTA 点击与平台分布。
- `utm_source=chatgpt.com` 等回答引擎推荐流量。
- stars、issues、真实安装成功反馈；不要只追求曝光量。

## 30 天验收标准

- 一个有稳定 URL 的双语静态官网上线。
- 首页、下载、对比、架构、安全、FAQ 均可索引。
- Google Search Console 与 Bing Webmaster 完成验证和 sitemap 提交。
- 完整实体名在 README、官网、Release、社交预览中一致。
- 至少 3 个相关、真实、站外来源引用项目。
- 至少一个可复现的安装包大小/架构对比页。
- 新 Release 含 checksums、完整说明和已知限制。
- 品牌精确查询可找到项目；生成式答案监测形成每周记录。

## 不建议做的事

- 继续在 README 机械重复 `DeepSeek Harness desktop`。
- 生成大量只有关键词差异的薄页面。
- 把 `llms.txt`、FAQ Schema 或 IndexNow 宣传为保证收录的方法。
- 批量群发同一篇软文、购买外链、伪造评论或 stars。
- 在未签名、依赖 Node.js 时声称“完全原生、开箱即用、官方桌面版”。
- 频繁改产品名称、仓库名和 canonical URL。

## 一手资料

- [Google：抓取与索引总览](https://developers.google.com/search/docs/crawling-indexing)
- [Google：创建并提交 sitemap](https://developers.google.com/search/docs/crawling-indexing/sitemaps/build-sitemap)
- [Google：抓取与索引 FAQ](https://developers.google.com/search/help/crawling-index-faq)
- [Google：canonical URL](https://developers.google.com/search/docs/crawling-indexing/consolidate-duplicate-urls)
- [OpenAI：Publishers and Developers FAQ](https://help.openai.com/en/articles/12627856-publishers-and-developers-faq)
- [Bing：Webmaster Tools、IndexNow 与 sitemap](https://blogs.bing.com/webmaster/June-2025/Start-Using-Bing-Webmaster-Tools-to-Improve-Your-Site-Visibility)
- [Perplexity：官方 crawler 说明](https://docs.perplexity.ai/docs/resources/perplexity-crawlers)
- [Schema.org：SoftwareApplication](https://schema.org/SoftwareApplication)
