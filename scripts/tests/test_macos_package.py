"""Exercise publication gates with fake Apple tools, never real credentials."""
import json
import os
from pathlib import Path
import plistlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "package-macos.sh"
FAKE_TOOL = r'''#!/usr/bin/env python3
import json, os, pathlib, shutil, sys
tool = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ["TRACE"], "a") as trace:
    trace.write(json.dumps([tool] + args) + "\n")
if tool == "file":
    print("Mach-O universal binary" if args[-1].endswith("dsh-desktop") else "data")
elif tool == "codesign" and "-dvv" in args:
    print("TeamIdentifier=" + os.environ.get("FAKE_TEAM", "TESTTEAM"))
    print("Authority=Developer ID Application: Test")
elif tool == "ditto":
    if "-x" in args:
        shutil.copytree(os.environ["APP"], pathlib.Path(args[-1]) / "dsh-desktop.app")
    else:
        pathlib.Path(args[-1]).touch()
elif tool == "hdiutil":
    pathlib.Path(args[-1]).touch()
elif tool == "xcrun" and args[:2] == ["notarytool", "submit"]:
    status = os.environ.get("NOTARY_STATUS", "Accepted")
    print(json.dumps({"id": "test-submission", "status": status}))
    sys.exit(1 if status == "In Progress" else 0)
elif tool == "xcrun" and args[:2] == ["notarytool", "log"]:
    pathlib.Path(args[-1]).write_text('{"issues": []}')
'''


@unittest.skipUnless(sys.platform == "darwin", "Packaging uses macOS PlistBuddy")
class MacOSPackageTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.app = self.root / "dsh-desktop.app"
        (self.app / "Contents/MacOS").mkdir(parents=True)
        (self.app / "Contents/MacOS/dsh-desktop").touch()
        (self.app / "Contents/Info.plist").write_bytes(plistlib.dumps({"CFBundleIdentifier": "com.example.test"}))
        self.output = self.root / "dist"
        self.trace = self.root / "trace"
        self.bin = self.root / "bin"
        self.bin.mkdir()
        for name in ("file", "lipo", "codesign", "ditto", "hdiutil", "xcrun", "spctl", "security"):
            tool = self.bin / name
            tool.write_text(FAKE_TOOL)
            tool.chmod(0o755)
        # Do not inherit signing or notarization secrets from a developer/CI environment.
        self.env = {key: value for key, value in os.environ.items()
                    if not key.startswith(("APPLE_", "MACOS_", "GITHUB_"))}
        self.env.update(PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                        RUNNER_TEMP=str(self.root), TRACE=str(self.trace), APP=str(self.app),
                        MACOS_SIGNING_IDENTITY="test-identity", APPLE_TEAM_ID="TESTTEAM",
                        APPLE_ID="test@example.invalid", APPLE_APP_SPECIFIC_PASSWORD="fake-password")

    def run_package(self, *args):
        result = subprocess.run(["bash", str(SCRIPT), str(self.app), str(self.output), *args],
                                env=self.env, capture_output=True, text=True)
        events = [json.loads(line) for line in self.trace.read_text().splitlines()] if self.trace.exists() else []
        return result, events

    def test_accepted_staples_app_before_final_zip_and_signs_dmg(self):
        result, events = self.run_package()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(list(self.output.iterdir())), 2)
        app_staple = next(i for i, e in enumerate(events) if e[:3] == ["xcrun", "stapler", "staple"])
        final_zip = next(i for i, e in enumerate(events) if e[0] == "ditto" and e[-1].endswith("darwin-universal.zip"))
        self.assertLess(app_staple, final_zip)
        self.assertEqual(events[app_staple][-1], str(self.app))
        self.assertEqual(sum(e[:3] == ["xcrun", "notarytool", "submit"] for e in events), 2)
        self.assertTrue(any(e[0] == "codesign" and "--sign" in e and e[-1].endswith(".dmg") for e in events))
        self.assertFalse(list(self.root.glob("dsh-signing.*")))

    def test_invalid_or_pending_notarization_never_publishes_or_staples(self):
        for status in ("Invalid", "In Progress"):
            with self.subTest(status=status):
                self.env["NOTARY_STATUS"] = status
                result, events = self.run_package()
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.output.exists())
                self.assertFalse(any(e[:3] == ["xcrun", "stapler", "staple"] for e in events))
                self.assertFalse(list(self.root.glob("dsh-signing.*")))
                self.trace.unlink()

    def test_wrong_team_refused_before_submission(self):
        self.env["FAKE_TEAM"] = "OTHERTEAM"
        result, events = self.run_package()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.output.exists())
        self.assertFalse(any(e[0] == "xcrun" for e in events))

    def test_ci_cannot_bypass_notarization(self):
        self.env["GITHUB_ACTIONS"] = "true"
        result, events = self.run_package("--sign-only")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(events, [])

    def test_missing_notary_credentials_cleans_imported_keychain(self):
        del self.env["APPLE_ID"]
        self.env.update(MACOS_CERTIFICATE_P12_BASE64="dGVzdA==", MACOS_CERTIFICATE_PASSWORD="fake-password")
        result, events = self.run_package()
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(any(e[:2] == ["security", "delete-keychain"] for e in events))
        self.assertFalse(self.output.exists())
        self.assertFalse(list(self.root.glob("dsh-signing.*")))


if __name__ == "__main__":
    unittest.main()
