package main

import (
	"net/http"
	"testing"
)

func TestIsPreinstalledPluginRoute(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// diff-review host routes (git ops / file read-write / AI review).
		{"/diff-review/status", true},
		{"/diff-review/apply", true},
		{"/diff-review/apply-hunk", true},
		{"/diff-review/commit", true},
		{"/diff-review/push", true},
		{"/diff-review/history", true},
		{"/diff-review/commit-diff", true},
		{"/diff-review/comments", true},
		{"/diff-review/branches", true},
		{"/diff-review/review", true},
		{"/diff-review/pr", true},
		{"/diff-review/repos", true},
		{"/diff-review/files", true},
		// file-changes host routes (reveal any absolute path + changes).
		{"/api/file-changes/reveal", true},
		{"/api/file-changes/changes", true},

		// Not blocked: unrelated paths and prefix boundaries.
		{"/", false},
		{"/api/status", false},
		{"/api/session.list", false},
		{"/diff-review", false},      // no trailing slash, not the blocked prefix
		{"/api/file-changes", false}, // no trailing slash, not the blocked prefix
		{"/diff-reviewary/status", false},
		{"/api/file-changes-extra/reveal", false},
		{"/other/diff-review/status", false}, // not a path prefix
	}
	for _, c := range cases {
		if got := isPreinstalledPluginRoute(c.path); got != c.want {
			t.Errorf("isPreinstalledPluginRoute(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestPreinstalledPluginRouteForbiddenOverRemote(t *testing.T) {
	m, base := newTestRemote(t)
	jwt := pairJWT(t, m, base)

	for _, path := range []string{
		"/diff-review/status",
		"/diff-review/files",
		"/api/file-changes/reveal",
		"/api/file-changes/changes",
	} {
		resp := authedReq(t, base, jwt, path)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s status = %d, want 403", path, resp.StatusCode)
		}
	}

	// A paired request to an unrelated route still passes through.
	resp := authedReq(t, base, jwt, "/api/session.list")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("non-plugin route status = %d, want 200", resp.StatusCode)
	}
}
