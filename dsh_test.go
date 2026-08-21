package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsSupportedNodeVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		version   string
		supported bool
	}{
		{name: "minimum supported 22", version: "v22.19.0", supported: true},
		{name: "newer 22", version: "v22.20.1", supported: true},
		{name: "older 22", version: "v22.18.9", supported: false},
		{name: "unsupported 23", version: "v23.11.1", supported: false},
		{name: "supported 24", version: "v24.0.0", supported: true},
		{name: "supported future major", version: "v25.0.0", supported: true},
		{name: "unsupported old major", version: "v20.20.0", supported: false},
		{name: "invalid version", version: "22.19.0", supported: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isSupportedNodeVersion(tt.version); got != tt.supported {
				t.Fatalf("isSupportedNodeVersion(%q) = %v, want %v", tt.version, got, tt.supported)
			}
		})
	}
}

func TestFindCompatibleNodeInstallationSkipsOlderNode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixtures use POSIX executable scripts")
	}
	t.Parallel()

	oldDir := filepath.Join(t.TempDir(), "node-20", "bin")
	newDir := filepath.Join(t.TempDir(), "node-24", "bin")
	writeExecutable(t, filepath.Join(oldDir, "node"), "#!/bin/sh\necho v20.20.0\n")
	writeExecutable(t, filepath.Join(oldDir, "npm"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(newDir, "node"), "#!/bin/sh\necho v24.3.0\n")
	writeExecutable(t, filepath.Join(newDir, "npm"), "#!/bin/sh\nexit 0\n")

	install, err := findCompatibleNodeInstallation([]string{oldDir, newDir})
	if err != nil {
		t.Fatalf("findCompatibleNodeInstallation() error = %v", err)
	}
	if install.nodePath != filepath.Join(newDir, "node") {
		t.Fatalf("nodePath = %q, want compatible runtime in %q", install.nodePath, newDir)
	}
	if install.npmPath != filepath.Join(newDir, "npm") {
		t.Fatalf("npmPath = %q, want npm beside selected Node", install.npmPath)
	}
	if install.version != "v24.3.0" {
		t.Fatalf("version = %q, want v24.3.0", install.version)
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
