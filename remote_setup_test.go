package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRemoteSetupQRCodeMatchesDeviceV1Contract(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	account, store := signedInAccountManager(t)
	server := &fakeRemoteSetupServer{challenge: remoteSetupChallenge{ID: "challenge-1", Token: "opaque-token", ExpiresAt: now.Add(5 * time.Minute)}}
	app := &App{account: account, remoteSetup: newRemoteSetupManager(account, server, store, func() time.Time { return now })}

	started, err := app.StartRemoteSetup()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := started.QRPayload, "dsh://pair?v=1&challenge=opaque-token"; got != want {
		t.Fatalf("QRPayload = %q, want %q", got, want)
	}

	tests := []struct {
		name    string
		payload string
		valid   bool
	}{
		{name: "valid v1", payload: started.QRPayload, valid: true},
		{name: "missing version", payload: "dsh://pair?challenge=opaque-token"},
		{name: "duplicate version", payload: "dsh://pair?v=1&v=1&challenge=opaque-token"},
		{name: "duplicate challenge", payload: "dsh://pair?v=1&challenge=opaque-token&challenge=second"},
		{name: "unknown parameter", payload: "dsh://pair?v=1&challenge=opaque-token&host=attacker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := acceptsDevicePairingV1Contract(tt.payload); got != tt.valid {
				t.Fatalf("acceptsDevicePairingV1Contract(%q) = %v, want %v", tt.payload, got, tt.valid)
			}
		})
	}
}

// acceptsDevicePairingV1Contract mirrors AccountPairingChallenge.parse in dsh-ios.
// It is deliberately test-only: Desktop emits Pairing QR payloads but never consumes them.
func acceptsDevicePairingV1Contract(payload string) bool {
	parsed, err := url.Parse(payload)
	if err != nil || parsed.Scheme != "dsh" || parsed.Host != "pair" || parsed.RawQuery == "" {
		return false
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || len(values) != 2 {
		return false
	}
	single := func(name string) (string, bool) {
		items, ok := values[name]
		returnValue := ""
		if ok && len(items) == 1 {
			returnValue = items[0]
		}
		return returnValue, ok && len(items) == 1 && returnValue != ""
	}
	version, versionOK := single("v")
	_, challengeOK := single("challenge")
	return versionOK && version == "1" && challengeOK
}

func TestOwnerCanStartAndCompleteRemoteSetup(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	account, store := signedInAccountManager(t)
	server := &fakeRemoteSetupServer{challenge: remoteSetupChallenge{ID: "challenge-1", Token: "single-use", ExpiresAt: now.Add(5 * time.Minute)}}
	app := &App{account: account, remoteSetup: newRemoteSetupManager(account, server, store, func() time.Time { return now })}

	started, err := app.StartRemoteSetup()
	if err != nil {
		t.Fatal(err)
	}
	if started.State != remoteSetupPending || started.QR == "" || strings.Contains(started.QRPayload, "access-token") {
		t.Fatalf("StartRemoteSetup() = %+v, want pending scannable QR without bearer credential", started)
	}
	server.result = remoteSetupResult{State: remoteSetupCompleted, Pairing: pairedDevice{PairingID: "pairing-1", DeviceID: "device-1", Name: "iPhone"}}
	completed, err := app.RefreshRemoteSetup()
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != remoteSetupCompleted || completed.QR != "" || len(completed.Devices) != 1 {
		t.Fatalf("RefreshRemoteSetup() = %+v, want completed Pairing and invalid QR", completed)
	}
	if _, err := app.RefreshRemoteSetup(); !errors.Is(err, errRemoteSetupNotPending) {
		t.Fatalf("replayed completed challenge error = %v, want not pending", err)
	}
	if got := app.RemoteSetupStatus(); len(got.Devices) != 1 {
		t.Fatalf("replay changed persisted Devices: %+v", got.Devices)
	}

	restarted := newRemoteSetupManager(account, server, store, func() time.Time { return now })
	got := restarted.status()
	if len(got.Devices) != 1 || got.Devices[0].DeviceID != "device-1" || got.State != remoteSetupCompleted {
		t.Fatalf("restart status = %+v, want persisted Device without repeated setup", got)
	}
}

func TestExpiredCanceledAndConsumedChallengesCannotBeReused(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	account, store := signedInAccountManager(t)
	server := &fakeRemoteSetupServer{challenge: remoteSetupChallenge{ID: "challenge-1", Token: "single-use", ExpiresAt: now.Add(time.Minute)}}
	manager := newRemoteSetupManager(account, server, store, func() time.Time { return now })
	if _, err := manager.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if got := manager.status(); got.State != remoteSetupExpired || got.QR != "" {
		t.Fatalf("expired status = %+v", got)
	}
	server.challenge = remoteSetupChallenge{ID: "challenge-2", Token: "replacement", ExpiresAt: now.Add(time.Minute)}
	if got, err := manager.start(context.Background()); err != nil || got.ChallengeID != "challenge-2" {
		t.Fatalf("replacement setup = (%+v, %v)", got, err)
	}
	if got, err := manager.cancel(context.Background()); err != nil || got.State != remoteSetupCanceled || got.QR != "" {
		t.Fatalf("cancel = (%+v, %v)", got, err)
	}
	if server.canceledID != "challenge-2" {
		t.Fatalf("canceled challenge = %q", server.canceledID)
	}
	server.result = remoteSetupResult{State: remoteSetupCompleted, Pairing: pairedDevice{PairingID: "pairing-2", DeviceID: "device-2", Name: "iPad"}}
	if _, err := manager.refresh(context.Background()); !errors.Is(err, errRemoteSetupNotPending) {
		t.Fatalf("refresh canceled challenge error = %v, want not pending", err)
	}
}

func TestFailedCancellationRetainsChallengeForRetryAndBlocksReplacement(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	account, store := signedInAccountManager(t)
	server := &fakeRemoteSetupServer{challenge: remoteSetupChallenge{ID: "challenge-1", Token: "single-use", ExpiresAt: now.Add(time.Minute)}, cancelErr: errors.New("offline")}
	manager := newRemoteSetupManager(account, server, store, func() time.Time { return now })
	if _, err := manager.start(context.Background()); err != nil {
		t.Fatal(err)
	}
	failed, err := manager.cancel(context.Background())
	if err == nil || failed.State != remoteSetupCancelFailed || failed.QR != "" || failed.ChallengeID != "challenge-1" {
		t.Fatalf("failed cancellation = (%+v, %v)", failed, err)
	}
	if _, err := manager.start(context.Background()); err == nil {
		t.Fatal("replacement challenge was created before old challenge was invalidated")
	}
	server.cancelErr = nil
	if got, err := manager.cancel(context.Background()); err != nil || got.State != remoteSetupCanceled {
		t.Fatalf("retry cancellation = (%+v, %v)", got, err)
	}
}

func TestExistingLANPairingIsRegisteredWithHostIdentityProof(t *testing.T) {
	account, store := signedInAccountManager(t)
	server := &fakeRemoteSetupServer{lanPairing: pairedDevice{PairingID: "pairing-lan", DeviceID: "lan-device", Name: "LAN Device"}}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	proof := fakeLANPairingProof{deviceID: "lan-device", publicKey: publicKey, privateKey: privateKey}
	app := &App{account: account, remoteSetup: newRemoteSetupManager(account, server, store, time.Now, proof)}

	status, err := app.RegisterLANPairing("lan-device", "LAN Device")
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Devices) != 1 || status.Devices[0].PairingID != "pairing-lan" {
		t.Fatalf("RegisterLANPairing() = %+v", status)
	}
	encodedPublicKey, err := base64.RawURLEncoding.DecodeString(server.lanIntent.HostLANPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(encodedPublicKey, server.lanIntent.signingBytes(), server.lanIntent.HostProof) {
		t.Fatal("LAN Pairing registration did not carry a valid existing Host proof")
	}
}

func TestLANProofAdapterNeverReplacesMissingExistingHostCredential(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(stateDirEnv, dir)
	registry := newDeviceRegistry(dir + "/devices.json")
	registry.register("lan-device", "LAN Device", "fingerprint")
	manager := newRemoteManager(nil)

	if _, err := manager.lanPublicKey("lan-device"); err == nil {
		t.Fatal("missing existing Host credential was silently replaced")
	}
	if _, err := os.Stat(dir + "/credential.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credential.json was created during proof: %v", err)
	}

	credential, err := newHostCredential()
	if err != nil {
		t.Fatal(err)
	}
	if err := credential.save(dir); err != nil {
		t.Fatal(err)
	}
	message := []byte("LAN Pairing registration intent")
	signature, err := manager.signLANPairing("lan-device", message)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(credential.PublicKey, message, signature) {
		t.Fatal("proof was not signed by the persisted Host LAN credential")
	}
}

type fakeLANPairingProof struct {
	deviceID   string
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
}

func (f fakeLANPairingProof) lanPublicKey(deviceID string) (string, error) {
	if deviceID != f.deviceID {
		return "", errors.New("not paired")
	}
	return base64.RawURLEncoding.EncodeToString(f.publicKey), nil
}

func (f fakeLANPairingProof) signLANPairing(deviceID string, message []byte) ([]byte, error) {
	if deviceID != f.deviceID {
		return nil, errors.New("not paired")
	}
	return ed25519.Sign(f.privateKey, message), nil
}

func signedInAccountManager(t *testing.T) (*accountManager, *memorySecretStore) {
	t.Helper()
	store := newMemorySecretStore()
	manager := newAccountManager(fakeAccountAuthorizer{token: "token"}, &fakeAccountServer{credential: validAccountCredential()}, store)
	if got := manager.signIn(context.Background()); got.State != accountStateSignedIn {
		t.Fatalf("sign in = %+v", got)
	}
	return manager, store
}

type fakeRemoteSetupServer struct {
	challenge  remoteSetupChallenge
	result     remoteSetupResult
	canceledID string
	lanIntent  lanPairingIntent
	lanPairing pairedDevice
	cancelErr  error
}

func (f *fakeRemoteSetupServer) createChallenge(context.Context, string, string) (remoteSetupChallenge, error) {
	return f.challenge, nil
}
func (f *fakeRemoteSetupServer) challengeResult(context.Context, string, string) (remoteSetupResult, error) {
	return f.result, nil
}
func (f *fakeRemoteSetupServer) cancelChallenge(_ context.Context, _ string, challengeID string) error {
	f.canceledID = challengeID
	return f.cancelErr
}
func (f *fakeRemoteSetupServer) registerLANPairing(_ context.Context, _ string, intent lanPairingIntent) (pairedDevice, error) {
	f.lanIntent = intent
	return f.lanPairing, nil
}
