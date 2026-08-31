#!/usr/bin/env bash
# Sign with an existing local identity or an encrypted CI P12; notarize by default.
set -euo pipefail
set +x
umask 077

app=${1:?Usage: package-macos.sh APP OUTPUT_DIR [--sign-only]}
output=${2:?An output directory is required}
mode=${3:-}
[[ -z "$mode" || "$mode" == --sign-only ]] || { echo 'Unknown packaging mode' >&2; exit 1; }
[[ "$mode" != --sign-only || "${GITHUB_ACTIONS:-}" != true ]] || {
  echo 'CI releases must be notarized' >&2; exit 1;
}
: "${MACOS_SIGNING_IDENTITY:?Select a Developer ID Application identity}"
: "${APPLE_TEAM_ID:?Set the expected Apple team ID}"
[[ -d "$app" && ! -e "$output" ]] || {
  echo 'App must exist and output directory must not already exist' >&2; exit 1;
}

work=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/dsh-signing.XXXXXX")
keychain=""
cleanup() {
  if [[ -n "$keychain" ]]; then security delete-keychain "$keychain" >/dev/null 2>&1 || true; fi
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
signing_args=(--keychain "${MACOS_KEYCHAIN_PATH:-$HOME/Library/Keychains/login.keychain-db}")
notary_args=()

if [[ -n "${MACOS_CERTIFICATE_P12_BASE64:-}" ]]; then
  : "${MACOS_CERTIFICATE_PASSWORD:?Missing P12 password}"
  keychain="$work/signing.keychain-db"
  keychain_password=$(openssl rand -hex 32)
  if [[ "${GITHUB_ACTIONS:-}" == true ]]; then echo "::add-mask::$keychain_password"; fi
  printf '%s' "$MACOS_CERTIFICATE_P12_BASE64" | base64 --decode > "$work/identity.p12"
  security create-keychain -p "$keychain_password" "$keychain"
  security set-keychain-settings -lut 21600 "$keychain"
  security unlock-keychain -p "$keychain_password" "$keychain"
  security import "$work/identity.p12" -k "$keychain" -P "$MACOS_CERTIFICATE_PASSWORD" -T /usr/bin/codesign >/dev/null
  security set-key-partition-list -S apple-tool:,apple:,codesign: -k "$keychain_password" "$keychain" >/dev/null
  signing_args=(--keychain "$keychain")
fi

if [[ "$mode" != --sign-only ]]; then
  if [[ -n "${APPLE_NOTARY_API_KEY_P8_BASE64:-}" ]]; then
    : "${APPLE_NOTARY_KEY_ID:?Missing Team API Key ID}"
    : "${APPLE_NOTARY_ISSUER_ID:?Missing Team API Issuer ID}"
    printf '%s' "$APPLE_NOTARY_API_KEY_P8_BASE64" | base64 --decode > "$work/notary.p8"
    notary_args=(--key "$work/notary.p8" --key-id "$APPLE_NOTARY_KEY_ID" --issuer "$APPLE_NOTARY_ISSUER_ID")
  else
    : "${APPLE_ID:?Missing notarization credentials: set Team API key or Apple ID and app-specific password}"
    : "${APPLE_APP_SPECIFIC_PASSWORD:?Missing Apple app-specific password}"
    notary_args=(--apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" --password "$APPLE_APP_SPECIFIC_PASSWORD")
  fi
fi

# Wails currently ships one Go executable. Stop if new embedded code needs its own signing policy.
executable="$app/Contents/MacOS/dsh-desktop"
while IFS= read -r -d '' candidate; do
  if [[ "$(file -b "$candidate")" == *Mach-O* && "$candidate" != "$executable" ]]; then
    echo "Unexpected embedded executable; define its signing policy first: $candidate" >&2
    exit 1
  fi
done < <(find "$app" -type f -print0)
lipo "$executable" -verify_arch arm64 x86_64
codesign --force --sign "$MACOS_SIGNING_IDENTITY" "${signing_args[@]}" --options runtime --timestamp "$app"
codesign --verify --deep --strict --verbose=2 "$app"
details=$(codesign -dvv "$app" 2>&1)
printf '%s\n' "$details"
printf '%s\n' "$details" | grep -Fxq "TeamIdentifier=$APPLE_TEAM_ID"
printf '%s\n' "$details" | grep -Fq 'Authority=Developer ID Application:'

notarize() {
  local artifact=$1 label=$2 staple_target=$3 submission_id status
  # Keep diagnostics separate from uploadable assets, including when Apple returns Invalid.
  if ! xcrun notarytool submit "$artifact" "${notary_args[@]}" --wait --timeout 30m \
      --output-format json > "$work/$label.json"; then
    echo "Notary submission did not complete successfully ($label)." >&2
  fi
  submission_id=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("id", ""))' "$work/$label.json")
  status=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("status", ""))' "$work/$label.json")
  echo "Notarization $label: submission=$submission_id status=$status"
  if [[ -n "$submission_id" ]]; then
    xcrun notarytool log "$submission_id" "${notary_args[@]}" "$work/$label-log.json" || true
    if [[ -f "$work/$label-log.json" ]]; then cat "$work/$label-log.json"; fi
  fi
  [[ "$status" == Accepted ]] || { echo 'Notarization not accepted; refusing to package release' >&2; return 1; }
  xcrun stapler staple "$staple_target"
  xcrun stapler validate "$staple_target"
}

if [[ "$mode" != --sign-only ]]; then
  ditto -c -k --sequesterRsrc --keepParent "$app" "$work/app-submit.zip"
  notarize "$work/app-submit.zip" app-submit "$app"
fi

mkdir "$work/assets"
zip="$work/assets/dsh-desktop-darwin-universal.zip"
dmg="$work/assets/dsh-desktop-darwin-universal.dmg"
ditto -c -k --sequesterRsrc --keepParent "$app" "$zip"
hdiutil create -volname 'DeepSeek Harness' -srcfolder "$app" -ov -format UDZO "$dmg"
bundle_id=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$app/Contents/Info.plist")
codesign --sign "$MACOS_SIGNING_IDENTITY" "${signing_args[@]}" --timestamp --identifier "$bundle_id.dmg" "$dmg"
codesign --verify --strict --verbose=2 "$dmg"
if [[ "$mode" != --sign-only ]]; then
  notarize "$dmg" dmg "$dmg"
  spctl --assess --type execute --verbose=4 "$app"
  spctl --assess --type open --context context:primary-signature --verbose=4 "$dmg"
fi

# Check the ZIP actually contains the final, signed (and normally stapled) app.
ditto -x -k "$zip" "$work/extracted"
extracted="$work/extracted/$(basename "$app")"
codesign --verify --deep --strict --verbose=2 "$extracted"
if [[ "$mode" != --sign-only ]]; then xcrun stapler validate "$extracted"; fi
mkdir -p "$(dirname "$output")"
mv "$work/assets" "$output"
if [[ "$mode" == --sign-only ]]; then
  echo "Signed assets (NOT notarized; Gatekeeper acceptance not established): $output"
else
  echo "Signed and notarized assets: $output"
fi
