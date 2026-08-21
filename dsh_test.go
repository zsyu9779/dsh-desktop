package main

import "testing"

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
