package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDenyRemote(t *testing.T) {
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
		{"/diff-review", false},      // no trailing slash, not the blocked prefix
		{"/api/file-changes", false}, // no trailing slash, not the blocked prefix
		{"/diff-reviewary/status", false},
		{"/api/file-changes-extra/reveal", false},
		{"/other/diff-review/status", false}, // not a path prefix
	}
	for _, c := range cases {
		if got := shouldDenyRemote(c.path); got != c.want {
			t.Errorf("shouldDenyRemote(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestRemoteProxyDeniesPreinstalledPluginRoutes(t *testing.T) {
	r := &remoteManager{enabled: true, token: "tok"}
	h := r.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// A paired request with a valid cookie to a denied route must be blocked.
	denied := httptest.NewRequest(http.MethodGet, "http://remote/diff-review/status", nil)
	denied.AddCookie(&http.Cookie{Name: remoteCookieName, Value: "tok"})
	deniedRec := httptest.NewRecorder()
	h.ServeHTTP(deniedRec, denied)
	if deniedRec.Code != http.StatusForbidden {
		t.Errorf("denied route status = %d, want %d", deniedRec.Code, http.StatusForbidden)
	}

	// A paired request to an unrelated route still passes through.
	allowed := httptest.NewRequest(http.MethodGet, "http://remote/api/status", nil)
	allowed.AddCookie(&http.Cookie{Name: remoteCookieName, Value: "tok"})
	allowedRec := httptest.NewRecorder()
	h.ServeHTTP(allowedRec, allowed)
	if allowedRec.Code != http.StatusOK {
		t.Errorf("allowed route status = %d, want %d", allowedRec.Code, http.StatusOK)
	}
}
