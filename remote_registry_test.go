package main

import (
	"net/http"
	"testing"
)

func TestListDevicesAfterPairing(t *testing.T) {
	m, base := newTestRemote(t)
	pairJWT(t, m, base)

	devs := m.listDevices()
	if len(devs) != 1 {
		t.Fatalf("list = %d devices, want 1", len(devs))
	}
	if devs[0].DeviceID == "" {
		t.Fatal("device has empty ID")
	}
}

func TestRevokeDeviceForbidden(t *testing.T) {
	m, base := newTestRemote(t)
	jwt := pairJWT(t, m, base)

	devs := m.listDevices()
	if len(devs) != 1 {
		t.Fatalf("list = %d devices, want 1", len(devs))
	}
	if !m.revokeDevice(devs[0].DeviceID) {
		t.Fatal("revoke returned false")
	}

	resp, _ := authedGet(t, base, jwt)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("revoked device status = %d, want 403", resp.StatusCode)
	}
}

func TestRevokeDoesNotAffectOtherDevice(t *testing.T) {
	m, base := newTestRemote(t)
	jwtA := pairJWT(t, m, base)

	devs := m.listDevices()
	if len(devs) != 1 {
		t.Fatalf("after first pair: %d devices, want 1", len(devs))
	}
	idA := devs[0].DeviceID

	// Pair a second device with a fresh code.
	m.regenerateToken()
	jwtB := pairJWT(t, m, base)

	if !m.revokeDevice(idA) {
		t.Fatal("revoke A failed")
	}

	respA, _ := authedGet(t, base, jwtA)
	if respA.StatusCode != http.StatusForbidden {
		t.Fatalf("revoked device A status = %d, want 403", respA.StatusCode)
	}
	respB, _ := authedGet(t, base, jwtB)
	if respB.StatusCode != http.StatusOK {
		t.Fatalf("device B status = %d, want 200", respB.StatusCode)
	}
}
