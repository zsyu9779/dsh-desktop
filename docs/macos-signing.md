# macOS release signing

The release workflow builds the app without credentials, then passes its ZIP to a separate
`sign-macos` job in the `macos-release` GitHub environment. Only its verified ZIP and DMG
reach GitHub Releases. Missing credentials, signing errors, or a notarization result other
than `Accepted` stop publication on all platforms.

The source job resolves an existing `v*` tag to a commit already on `origin/main`.
The signer uses scripts from the workflow revision, not from the requested historical tag.
Restrict the environment to the `main` branch and `v*` tags, and restrict who can push those
refs. Repository administrators should also protect workflow changes and consider required
reviewers for the environment. Tag filtering alone does not replace repository access control.

## Environment configuration

Create these in **Settings → Environments → macos-release**:

| Kind | Name | Value |
| --- | --- | --- |
| Secret | `MACOS_CERTIFICATE_P12_BASE64` | Base64 of an encrypted export of the selected local Developer ID identity, including its public intermediate CA |
| Secret | `MACOS_CERTIFICATE_PASSWORD` | Random password for that export, not the Mac login password |
| Variable | `MACOS_SIGNING_IDENTITY` | SHA-1 fingerprint of the selected Developer ID Application certificate |
| Variable | `APPLE_TEAM_ID` | Expected signing team |

Configure **one** notarization method as well:

| Method | Secrets | Variables |
| --- | --- | --- |
| Team API key | `APPLE_NOTARY_API_KEY_P8_BASE64` | `APPLE_NOTARY_KEY_ID`, `APPLE_NOTARY_ISSUER_ID` |
| Apple Account | `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD` | Uses `APPLE_TEAM_ID` above |

The temporary CI keychain password is generated per run. Certificate/private-key material
stays outside the checkout and release directory, is never cached, and is removed on exit.
An unconditional workflow cleanup also handles interrupted signing steps. The user's login
keychain is not modified by the packaging script when using an existing local identity.

## Local use

Build first with `wails build -platform darwin/universal -clean`. Set `MACOS_SIGNING_IDENTITY`
and `APPLE_TEAM_ID`, then run:

```bash
bash scripts/package-macos.sh build/bin/dsh-desktop.app build/bin/notarized
```

The normal mode also requires notarization credentials. To verify signing using the local
keychain before those credentials are available, explicitly request:

```bash
bash scripts/package-macos.sh build/bin/dsh-desktop.app build/bin/signed-local --sign-only
```

The destination must not already exist. `--sign-only` is rejected inside GitHub Actions.
These local files are signed but **not notarized**; they are not evidence of Gatekeeper
acceptance and must not replace a notarized public release.

The app must contain the expected universal Go executable. Additional Mach-O files fail
closed until their signing/entitlement policy is defined. The app is signed with hardened
runtime and a secure timestamp, without blanket JIT or library-validation exceptions.

## Verification

`python3 -B -m unittest discover -s scripts/tests -v` exercises the publication gates using
fake Apple tools on macOS. Real packaging also checks architectures, Developer ID identity,
team, signatures, notarization tickets, and the app extracted from the final ZIP. Normal
mode assesses the app and DMG with Gatekeeper. Submission IDs and Apple diagnostic logs
are printed for troubleshooting; credentials are not. A timeout refuses publication even
if Apple continues processing the submission.

Before publishing the first notarized release, test browser-downloaded ZIP and DMG on Intel
and Apple Silicon Macs, retaining quarantine. Test offline Gatekeeper checks separately from
online first-time npm installation, plus Harness startup, exit, and LAN pairing. Automated
signature verification alone does not establish those runtime behaviors.

See [the research and official sources](macos-ci-signing-research.md) for certificate and
notarization background. No provisioning profile or Developer ID Installer certificate is
needed for the current plain `.app`/`.dmg` distribution.
