package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPSFingerprintMatches(t *testing.T) {
	m, _ := newTestRemote(t)
	fp := m.status().CertFingerprint
	if fp == "" {
		t.Fatal("no cert fingerprint advertised")
	}

	conn, err := tls.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", m.status().Port), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("tls dial: %v", err)
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		t.Fatal("no peer certificate")
	}
	sum := sha256.Sum256(state.PeerCertificates[0].Raw)
	if got := hex.EncodeToString(sum[:]); got != fp {
		t.Fatalf("fingerprint mismatch: got %s want %s", got, fp)
	}
}

// TestLeafCertStableAcrossEnable verifies the LAN leaf fingerprint does not
// rotate when the remote is re-enabled on the same IP (ticket 04): the phone's
// TOFU pin survives host restarts without a re-pair.
func TestLeafCertStableAcrossEnable(t *testing.T) {
	t.Setenv(stateDirEnv, t.TempDir())
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	t.Cleanup(upstream.Close)

	m := newRemoteManager(nil)
	if _, err := m.enable(upstream.URL); err != nil {
		t.Fatalf("first enable: %v", err)
	}
	first := m.status().CertFingerprint
	if first == "" {
		t.Fatal("no fingerprint after first enable")
	}

	m.disable()
	if _, err := m.enable(upstream.URL); err != nil {
		t.Fatalf("second enable: %v", err)
	}
	second := m.status().CertFingerprint

	if first != second {
		t.Fatalf("leaf fingerprint rotated across enable: %s -> %s", first, second)
	}
}

// TestIssueLeafCertReusesPersistedLeaf verifies the pure credential path:
// issuing twice for the same IP returns the identical fingerprint and PEM.
func TestIssueLeafCertReusesPersistedLeaf(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(stateDirEnv, dir)

	cred, err := loadOrCreateCredential(dir)
	if err != nil {
		t.Fatalf("loadOrCreateCredential: %v", err)
	}
	ip := net.ParseIP("192.0.2.10")

	_, _, fp1, err := cred.issueLeafCert(ip)
	if err != nil {
		t.Fatalf("first issue: %v", err)
	}
	if err := cred.save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reload from disk to simulate a restart.
	reloaded, err := loadOrCreateCredential(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	_, _, fp2, err := reloaded.issueLeafCert(ip)
	if err != nil {
		t.Fatalf("second issue: %v", err)
	}

	if fp1 != fp2 {
		t.Fatalf("fingerprint changed after reload: %s -> %s", fp1, fp2)
	}
}

// TestIssueLeafCertReissuesOnIPChange verifies a changed LAN IP produces a new
// fingerprint (the persisted leaf is scoped to its SAN IP).
func TestIssueLeafCertReissuesOnIPChange(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(stateDirEnv, dir)

	cred, err := loadOrCreateCredential(dir)
	if err != nil {
		t.Fatalf("loadOrCreateCredential: %v", err)
	}
	_, _, fp1, err := cred.issueLeafCert(net.ParseIP("192.0.2.10"))
	if err != nil {
		t.Fatalf("first issue: %v", err)
	}
	_, _, fp2, err := cred.issueLeafCert(net.ParseIP("192.0.2.20"))
	if err != nil {
		t.Fatalf("second issue: %v", err)
	}
	if fp1 == fp2 {
		t.Fatal("expected a new fingerprint when the SAN IP changes")
	}
}
