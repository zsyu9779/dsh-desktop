package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPollingEntitlementStreamUsesPersistedCredential(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/subscription/status" || request.Header.Get("Authorization") != "Bearer host-access-token" {
			http.Error(response, "bad request", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"state": "active", "accountID": "account-a", "expiresAt": time.Now().Add(time.Hour),
		})
	}))
	defer service.Close()
	secrets := newMemorySecretStore()
	account := newAccountManager(fakeAccountAuthorizer{}, &fakeAccountServer{}, secrets)
	if err := account.saveCredential(accountCredential{AccountID: "account-a", AccessToken: "host-access-token", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	account.restoreStatus()
	stream := newPollingEntitlementStream(account, newHTTPAccountServer(service.URL), time.Hour)
	updates := make(chan entitlementUpdate, 1)
	down := make(chan struct{}, 1)
	cancel := stream.subscribe(func(update entitlementUpdate) { updates <- update }, func() { down <- struct{}{} })
	defer cancel()
	select {
	case update := <-updates:
		if update.State != entitlementActive || update.AccountID != "account-a" {
			t.Fatalf("update = %+v", update)
		}
	case <-down:
		t.Fatal("stream reported offline")
	case <-time.After(time.Second):
		t.Fatal("stream did not poll immediately")
	}
}
