package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLocalDeveloperAccountAuthorizer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev-token")
	if err := os.WriteFile(path, []byte("developer-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DSH_DEV_IDENTITY_TOKEN_FILE", path)
	authorizer, ok := loadLocalDeveloperAccountAuthorizer()
	if !ok {
		t.Fatal("developer authorizer was not loaded")
	}
	token, err := authorizer.authorize(context.Background())
	if err != nil || token != "developer-token" {
		t.Fatalf("authorize() = (%q, %v)", token, err)
	}
}
