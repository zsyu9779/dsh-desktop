package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
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
