# dsh-account-login

Host Account 邮箱/密码注册与登录（dsh 设置页插件）。

- server 半（lib/index.js）：同源 /api/account-login/exchange（register|login）与
  /api/account-login/sign-out；账号交换指向 DSH_ACCOUNT_SERVER_URL（缺省
  https://relay.codegoround.com），成功后把 24h 会话写入桌面壳监听的本机会话桥
  （DSH_ACCOUNT_BRIDGE_DIR，缺省 ~/.dsh-desktop/account-bridge/session.json）。
- client 半（lib/client.js）：注册到 dsh 设置 -> 插件页的卡片。
- 桌面壳 watchAccountBridge 采用凭据 -> 登记 Host -> 启动 Relay（见
  dsh-desktop/account_bridge.go）。

安全：无两步验证、无找回；密码仅存在于请求与账号 server 之间，不落盘。
测试环境允许 http（无域名/明文），生产保持 https。
