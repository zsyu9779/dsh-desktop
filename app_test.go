package main

import (
	"strings"
	"testing"
)

func TestValidateExternalURL(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "https", raw: "https://example.com/docs?q=1#top", want: "https://example.com/docs?q=1#top"},
		{name: "http loopback with port", raw: "http://127.0.0.1:1234/x", want: "http://127.0.0.1:1234/x"},
		{name: "surrounding whitespace", raw: "  https://example.com  ", want: "https://example.com"},
		{name: "javascript scheme", raw: "javascript:alert(1)", wantErr: true},
		{name: "file scheme", raw: "file:///etc/passwd", wantErr: true},
		{name: "data scheme", raw: "data:text/html,<h1>x</h1>", wantErr: true},
		{name: "ftp scheme", raw: "ftp://example.com/x", wantErr: true},
		{name: "scheme-relative", raw: "//example.com/x", wantErr: true},
		{name: "missing host", raw: "https://", wantErr: true},
		{name: "plain text", raw: "not a url", wantErr: true},
		{name: "empty", raw: "", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateExternalURL(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateExternalURL(%q) = %q, want an error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateExternalURL(%q) returned error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("validateExternalURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// The bridge must reject a bad URL before it ever needs a Wails context, and a
// good one must still refuse to launch while the host has not started.
func TestOpenExternalURLRejectsBeforeLaunching(t *testing.T) {
	app := &App{}

	if err := app.OpenExternalURL("javascript:alert(1)"); err == nil {
		t.Fatal("OpenExternalURL accepted a javascript: URL")
	}

	err := app.OpenExternalURL("https://example.com")
	if err == nil || !strings.Contains(err.Error(), "Host") {
		t.Fatalf("OpenExternalURL with no context = %v, want a not-started error", err)
	}
}
