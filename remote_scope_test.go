package main

import (
	"net/http"
	"testing"
)

func authedReq(t *testing.T, base, jwt, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", base+path, nil)
	req.AddCookie(&http.Cookie{Name: remoteCookieName, Value: jwt})
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	defer resp.Body.Close()
	return resp
}

func TestRemoteScopeDeniesUnlistedEndpoints(t *testing.T) {
	m, base := newTestRemote(t)
	jwt := pairJWT(t, m, base)

	// DSH 0.1.2 removed the upstream privileged-method list, so the shell owns
	// the policy. Everything outside the allowlist is refused, including a
	// namespace a future release adds.
	for _, path := range []string{
		"/api/settings/describe",
		"/api/credentials/describe",
		"/api/agentPresets/read",
		"/api/directoryPicker/pick",
		"/api/dynamicCordisRunner/invoke",
		"/api/pluginInventory/list",
		"/api/llm/discoverModels",
		"/api/session/openWorkspacePath",
		"/api/workspaceFiles/read",
		"/api/subagents/prompt",
		"/api/brandnew/endpoint",
	} {
		if resp := authedReq(t, base, jwt, path); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s status = %d, want 403", path, resp.StatusCode)
		}
	}
}

func TestRemoteScopeAllowsListedEndpoints(t *testing.T) {
	m, base := newTestRemote(t)
	jwt := pairJWT(t, m, base)

	for _, path := range []string{
		"/api/session/list",
		"/api/session/prompt",
		"/api/session/uploadFileBinary",
		"/api/workspace/create",
		"/api/directoryPicker/list",
		"/api/agentPresets/list",
		"/api/llm/listProviders",
		"/api/goals/get",
		"/api/remote.mux",
		"/api/$events/result",
		"/assets/app.js",
	} {
		if resp := authedReq(t, base, jwt, path); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, resp.StatusCode)
		}
	}
}

func TestRemoteScopeGrantAllowsEverything(t *testing.T) {
	m, base := newTestRemote(t)
	jwt := pairJWT(t, m, base)
	m.setAllowPrivileged(true)

	for _, path := range []string{"/api/settings/describe", "/api/dynamicCordisRunner/invoke"} {
		if resp := authedReq(t, base, jwt, path); resp.StatusCode != http.StatusOK {
			t.Fatalf("%s after grant status = %d, want 200", path, resp.StatusCode)
		}
	}
}
