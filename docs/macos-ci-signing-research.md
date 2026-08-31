# macOS CI 签名与公证研究

研究日期：2026-08-31。基线：`f27fbd5ebdd8321754c02302e714e779016de75f`。本次只研究和记录，没有修改 workflow、导入凭据、签名、公证或发布新版本。

这是最初的研究快照；后续实现及实际所需配置以 [macOS 签名操作说明](macos-signing.md) 为准。

## 结论

现有 GitHub Actions 可以继续构建 Wails universal app；在打包发布前增加 **Developer ID Application 签名 → Apple 公证 → staple 票据 → 验证** 即可形成外部分发链路。单纯 `codesign` 不等于公证，ZIP 容器本身也不能 staple；最终 ZIP 必须重新装入已 staple 的 app。签名确认发布者及内容完整性，公证是 Apple 自动检查，不是 App Store 上架审核。[Apple 分发要求](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution)、[Apple 自定义公证流程](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow)

建议保留 ZIP 与 DMG 两种下载格式：先公证并 staple app，然后生成最终 ZIP；由同一份 app 创建 DMG，再签名、公证并 staple DMG。这样 ZIP 中的 app 和 DMG 容器都有自己的票据。两次提交是本项目同时支持两种交付物的实现选择，不是所有 DMG 发布都必须提交两次。

## 仓库实查

- [.github/workflows/release.yml](../.github/workflows/release.yml)：`macos-latest`，`darwin/universal`，Wails CLI `v2.11.0`；构建与测试后直接 `ditto` ZIP、`hdiutil` DMG，再上传 artifacts 并发布。没有导入证书、`codesign`、`notarytool`、`stapler`。
- [最近查到的 Release run](https://github.com/zsyu9779/dsh-desktop/actions/runs/32507269686) 成功，仅能证明原有流水线成功，不能证明产物经过 Developer ID 签名或公证；本次未下载该 run 的产物验证。
- `gh secret list`、`gh variable list` 成功但输出为空：当前可见的 repository 级配置为空，未确认 organization/environment 级配置；不能据此认定开发者没有证书。
- 同步后按顺序执行 `npm run build`（frontend）和 `go test ./...` 均通过；这些检查不包含签名、公证和 Gatekeeper 验证。
- [wails.json](../wails.json) 当前版本 `0.1.7`；[Info.plist](../build/darwin/Info.plist) 使用 `com.wails.{{.Name}}` 模板。建议首次正式签名前确定自有且稳定的 bundle ID，并在构建完成后、签名前校验实际值及版本，不能在签名后修改 plist。
- [dsh.go](../dsh.go) 检查用户机器的 Node/npm 并通过 `exec.CommandContext` 启动外部进程。根据当前代码，没有理由照搬 Electron 的 JIT entitlement；最终是否需要例外必须以实际 bundle 和运行测试为准。app 的签名不会为外部 Node、下载的 npm 包或整个运行链提供担保。

## 账号和证书前置

1. 准备有效的 Apple Developer Program 会员及相应团队权限。Account Holder 创建可导出的 **Developer ID Application** 证书：在 Mac 上生成 CSR，申请并安装 `.cer`，再从持有配对私钥的钥匙串导出带密码的 `.p12`。只有 `.cer` 不够，CI 需要私钥。云管理证书与这里的本地导出路线不同。[Developer ID 申请](https://developer.apple.com/help/account/certificates/create-developer-id-certificates/)、[导出签名证书](https://help.apple.com/xcode/mac/current/en.lproj/dev154b28f09.html)
2. app 和 DMG 都用 Developer ID Application；**Developer ID Installer 只用于安装器 `.pkg`**，当前不需要。普通 Developer ID app 不因签名本身就需要 provisioning profile；以后加入 CloudKit 等受管能力时再评估。[证书类型与错误排查](https://developer.apple.com/documentation/security/resolving-common-notarization-issues)、[Developer ID 能力要求](https://developer.apple.com/support/developer-id/)
3. 另行准备公证认证。签名 `.p12` 与公证 `.p8`/密码是两件不同的凭据，不可互相替代。

## 公证认证选择

| 方式 | 输入 | 适用与代价 |
| --- | --- | --- |
| 推荐：App Store Connect **Team API key** | `.p8`、Key ID、Issuer ID | 不依赖个人 Apple Account 密码，适合长期 CI；需要 Account Holder 先开通 API，由 Account Holder/Admin 创建 Team key，按实际需要授予角色。Team key 不能限制到单一 app，需专用密钥和严格保管。 |
| 备选：Apple Account | Apple ID、Team ID、app-specific password | 初次接入直接，但依赖账号；需要双因素认证，修改或重置账号主密码会撤销所有 app-specific passwords。不要存账号主密码。 |

参数组合分别是 `--key PATH --key-id ID --issuer ISSUER` 与 `--apple-id ID --team-id TEAM --password APP_PASSWORD`。本方案统一使用 Team key：检索到的 Apple API key 文档仍列出 Individual key 的 `notaryTool` 限制，而实际兼容性还应核对选定 Xcode/notarytool 版本；本次不验证 Individual key，也不把它当成等价替代。[Apple notarytool 认证说明](https://developer.apple.com/documentation/technotes/tn3147-migrating-to-the-latest-notarization-tool)、[API key 类型限制](https://developer.apple.com/documentation/appstoreconnectapi/creating-api-keys-for-app-store-connect-api)、[创建 Team key](https://developer.apple.com/help/app-store-connect/get-started/app-store-connect-api)、[app-specific password](https://support.apple.com/en-gb/102654)

建议在 GitHub 的受保护发布 environment 配置以下名字；它们是**待配置方案**，不是已存在的配置。

| 类型 | 名称 | 内容/条件 |
| --- | --- | --- |
| Secret | `MACOS_CERTIFICATE_P12_BASE64` | 含证书和私钥的 p12，经 base64 编码；编码不是加密 |
| Secret | `MACOS_CERTIFICATE_PASSWORD` | p12 导出密码 |
| Secret | `MACOS_KEYCHAIN_PASSWORD` | CI 临时钥匙串密码；也可改成每个 job 随机生成并遮罩 |
| Variable | `MACOS_SIGNING_IDENTITY` | 完整 `Developer ID Application: … (TEAMID)`，或选定证书指纹 |
| Variable | `APPLE_TEAM_ID` | 预期签名团队；Apple ID 公证路线也使用它 |
| Variable | `MACOS_BUNDLE_ID` | 产品确认后的稳定 app ID；这里只做校验，不能只配变量却不改构建模板 |
| Secret | `APPLE_NOTARY_API_KEY_P8_BASE64` | 推荐路线：Team API 私钥 p8 的 base64 |
| Variable | `APPLE_NOTARY_KEY_ID` | 推荐路线：Key ID |
| Variable | `APPLE_NOTARY_ISSUER_ID` | 推荐路线：Issuer ID |
| Secret | `APPLE_ID` | 备选路线：Apple Account 邮箱，无需与 API 路线同时配置 |
| Secret | `APPLE_APP_SPECIFIC_PASSWORD` | 备选路线：专用于公证的 app-specific password |

## CI 接入顺序

1. **校验发布来源及配置**：只接受可信 tag/commit；明确要求签名的 release 缺少 secrets 时立即失败，不能静默发布 unsigned 包。校验 Xcode/notarytool 版本并记录到日志；后续宜固定已验证的 runner/Xcode 组合，降低 `macos-latest` 漂移风险。
2. **构建与测试**：沿用 Wails `darwin/universal`；在签名前完成版本、图标、plist 等全部修改。检查最终 bundle 中的 Mach-O、framework、helper；universal 合并必须发生在签名前。
3. **导入临时钥匙串**：在 `$RUNNER_TEMP` 解码 p12、创建并解锁专用 keychain、导入证书、设置 key partition list。使用 `security find-identity -v -p codesigning` 验证指定 identity 可用。GitHub 有官方导入示例；本项目不需要照抄其 provisioning profile 部分。[GitHub macOS 证书导入](https://docs.github.com/en/actions/how-tos/deploy/deploy-to-third-party-platforms/sign-xcode-applications)
4. **由内到外签名**：若有嵌套代码，逐个按类型和必要 entitlement 签名，最后签 app。启用 hardened runtime 和 secure timestamp；不启用调试 `get-task-allow`。不要用 `--deep` 一次性签名，它可能遗漏非标准位置代码或错用 entitlement；`--deep` 可以用于验证。[Apple 手工分发签名](https://developer.apple.com/documentation/xcode/creating-distribution-signed-code-for-the-mac/)、[签名与验证区别](https://developer.apple.com/library/archive/documentation/Security/Conceptual/CodeSigningGuide/Procedures/Procedures.html)
5. **公证 app**：创建临时提交 ZIP；`notarytool submit --wait --output-format json`；显式判断结果为 `Accepted`，保存 submission ID 和公证日志。随后 staple app 并验证票据。
6. **生成最终资产**：从已 staple app 重新生成最终 ZIP；由该 app 创建只读 UDZO DMG，用 Developer ID Application 签 DMG，再提交 DMG、公证通过、staple DMG。DMG 的签名 identifier 应与 app ID 区分，例如 app ID 加 `.dmg`。[Apple DMG 打包要求](https://developer.apple.com/documentation/xcode/packaging-mac-software-for-distribution)
7. **验证后上传**：只有最终验证完成才上传 `dist/`；原有依赖 `needs: build` 的 release job 可继续阻止失败构建发布。中间提交 ZIP、公证诊断、p12/p8 不放进 `dist/`，防止被 `files: artifacts/**` 误发布。
8. **无条件清理**：单独 `if: always()` 步骤删除临时 keychain、p12、p8；如果更改搜索列表则恢复它。GitHub 托管 runner 会销毁 VM，但显式清理有助于控制凭据生命周期；自托管 runner 尤其需要清理。

下面是关键命令骨架，**不是已经集成或通过实测的完整脚本**。实现时通过 step `env` 注入 secrets，不在 `run` 中插值 secret，关闭 shell tracing，并为各命令加入失败处理与清理。

```bash
# 在 macOS runner 上；下列环境变量应由对应 secrets/vars 注入。
umask 077
KEYCHAIN_PATH="$RUNNER_TEMP/dsh-signing.keychain-db"
CERT_PATH="$RUNNER_TEMP/dsh-certificate.p12"
NOTARY_KEY_PATH="$RUNNER_TEMP/dsh-notary.p8"
printf '%s' "$MACOS_CERTIFICATE_P12_BASE64" | base64 --decode > "$CERT_PATH"
security create-keychain -p "$MACOS_KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security set-keychain-settings -lut 21600 "$KEYCHAIN_PATH"
security unlock-keychain -p "$MACOS_KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security import "$CERT_PATH" -k "$KEYCHAIN_PATH" \
  -P "$MACOS_CERTIFICATE_PASSWORD" -T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple:,codesign: \
  -k "$MACOS_KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security find-identity -v -p codesigning "$KEYCHAIN_PATH"

# 推荐的 Team API key 路线；认证 profile 同样放在临时 keychain。
printf '%s' "$APPLE_NOTARY_API_KEY_P8_BASE64" | base64 --decode > "$NOTARY_KEY_PATH"
xcrun notarytool store-credentials dsh-notary --keychain "$KEYCHAIN_PATH" \
  --key "$NOTARY_KEY_PATH" --key-id "$APPLE_NOTARY_KEY_ID" \
  --issuer "$APPLE_NOTARY_ISSUER_ID"

APP="build/bin/dsh-desktop.app"
ZIP="dist/dsh-desktop-darwin-universal.zip"
DMG="dist/dsh-desktop-darwin-universal.dmg"

# 前提：已识别并签好所有嵌套代码；主 app 默认不加运行时例外。
codesign --force --sign "$MACOS_SIGNING_IDENTITY" --keychain "$KEYCHAIN_PATH" \
  --options runtime --timestamp "$APP"
codesign --verify --deep --strict --verbose=2 "$APP"
ditto -c -k --sequesterRsrc --keepParent "$APP" "$RUNNER_TEMP/app-submit.zip"
xcrun notarytool submit "$RUNNER_TEMP/app-submit.zip" \
  --keychain-profile dsh-notary --keychain "$KEYCHAIN_PATH" \
  --wait --timeout 30m --output-format json > "$RUNNER_TEMP/app-notary.json"
# 必须在此解析 JSON、要求 status == Accepted、下载并检查 log；否则停止。
xcrun stapler staple "$APP"
xcrun stapler validate "$APP"

mkdir -p dist
ditto -c -k --sequesterRsrc --keepParent "$APP" "$ZIP"
hdiutil create -volname "DeepSeek Harness" -srcfolder "$APP" -ov -format UDZO "$DMG"
codesign --sign "$MACOS_SIGNING_IDENTITY" --keychain "$KEYCHAIN_PATH" \
  --timestamp --identifier "${MACOS_BUNDLE_ID}.dmg" "$DMG"
xcrun notarytool submit "$DMG" \
  --keychain-profile dsh-notary --keychain "$KEYCHAIN_PATH" \
  --wait --timeout 30m --output-format json > "$RUNNER_TEMP/dmg-notary.json"
# 再次要求 Accepted 并检查 log；不得省略状态门禁。
xcrun stapler staple "$DMG"
xcrun stapler validate "$DMG"
```

认证、profile、等待及 JSON 参数已通过本机 `xcrun notarytool help submit`、`help store-credentials` 只读核对；没有用真实凭据运行以上骨架。公证超时不表示 Apple 停止处理，应保留 submission ID，通过 `info`/`wait` 查询，避免盲目重复上传；失败时用 `notarytool log ID` 下载原因，成功日志也要检查 warnings。[Apple 公证流程](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow)

## 验收与风险

- 签名结构：`codesign --verify --deep --strict --verbose=2 APP`；`codesign -dvvv APP` 检查预期 TeamIdentifier、identity、runtime flag、timestamp；`lipo -archs APP/Contents/MacOS/dsh-desktop` 应包含 `arm64 x86_64`。DMG 同样检查签名。
- 票据与策略：app、DMG 均 `xcrun stapler validate`；`spctl --assess --type execute --verbose=4 APP`；DMG 用 `spctl -a -t open --context context:primary-signature -v DMG`。签名检查不能替代 Gatekeeper 的真实安装测试。[Apple 签名深入说明](https://developer.apple.com/library/archive/technotes/tn2206/_index.html)
- 最终包：把最终 ZIP 解压到新目录，再检查其 app；挂载最终 DMG 后将 app 拖到 `/Applications`，分别在 Intel 和 Apple Silicon 机器做首次启动测试。通过浏览器下载 release 资产以保留 quarantine，验证离线首次打开时的 Gatekeeper 检查；再联网验证外部 Node 查找、Harness 启动和 UI 加载。公证票据支持离线校验，不代表首次下载 npm 依赖可以离线完成。不要用删除 quarantine 或关闭 Gatekeeper 来宣告通过。
- 权限：hardened runtime 不等于 App Sandbox。仅在可复现的签名后运行故障证明需要时加最小 entitlement；不要因为外部 Node 使用 JIT 就给 Go 主 app 加 `allow-jit`、`allow-unsigned-executable-memory` 或 `disable-library-validation`。[Apple runtime 要求与例外](https://developer.apple.com/documentation/security/resolving-common-notarization-issues)
- 信任边界：现有 `workflow_dispatch.release_tag` 直接用于 checkout，说明文字“existing tag”不是校验。接入凭据前应校验 ref 是允许的发布 tag，并解析为可信 commit；限制谁能创建发布 tag、变更 workflow 和读取发布 environment。构建脚本/依赖本身能执行代码，不可信源码不能进入带签名凭据的 job。可考虑独立签名 job，但必须验证其输入产物来源。
- 凭据保护：p12/p8 不提交、不缓存、不上传 artifacts；给 API key 和证书建立轮换与到期检查。证书被撤销可能影响已发布应用，不能把随意吊销当常规清理。[Developer ID 有效性](https://developer.apple.com/support/developer-id/)
- 当前未知：真实证书/私钥是否可用、团队与 API 权限、首次公证耗时、最终 universal bundle 内容及 entitlement 需求、双架构用户环境是否通过。需要首次受控签名构建验证后，才能说 CI 已能稳定产出签名公证包。
