# 原生 app 是唯一交付物；PWA 仅作验证工具

M1 用 PWA（复用 dsh Web UI）验证了「手机远程操控」可行。但订阅付费（Apple IAP）与可靠推送（APNs）都必须原生 app，若把 PWA 也作为对外交付物，就要同时维护两个客户端形态。因此决定：对外交付的只有一个原生 app —— 免费档为局域网（免账号），订阅档为注册登录 + 公网 relay；PWA 只在内部作为验证远程方案、测试服务端逻辑的工具，不作为交付里程碑。

这修订了 ADR-0002 中「PWA 先行」的措辞：免费局域网免账号的结论不变，只是承载形态从 PWA 改为原生 app。同时否决「跨 agent 统一入口」（Claude Code / Codex）：v1 仅适配 dsh。

_Status: accepted_
