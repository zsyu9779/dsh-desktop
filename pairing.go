package main

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// stateDirEnv overrides where the Host persists its signing credential and
// device registry. Tests point it at a temp dir because the default lives
// under the user's home, outside the workspace the file sandbox lets us write.
const stateDirEnv = "DSH_DESKTOP_STATE"

// deviceIdentity is a Device registered on this Host.
type deviceIdentity struct {
	DeviceID    string    `json:"deviceId"`
	Name        string    `json:"name"`
	PublicKeyFP string    `json:"publicKeyFp"`
	IssuedAt    time.Time `json:"issuedAt"`
	LastActive  time.Time `json:"lastActive"`
}

// hostCredential is the Host's persisted signing material: an Ed25519 keypair
// used to sign device JWTs, plus a short-lived CA for the later LAN TLS ticket.
type hostCredential struct {
	Seed      []byte `json:"seed"`
	PublicKey []byte `json:"publicKey"`
	CACertPEM []byte `json:"caCertPem"`
	CAKeyPEM  []byte `json:"caKeyPem"`
}

func (c *hostCredential) privateKey() ed25519.PrivateKey { return ed25519.NewKeyFromSeed(c.Seed) }

func (c *hostCredential) publicKeyB64() string {
	return base64.RawURLEncoding.EncodeToString(c.PublicKey)
}

// jwtClaims is the payload of the short-lived device token.
type jwtClaims struct {
	DeviceID string `json:"deviceId"`
	Scope    string `json:"scope"`
	Exp      int64  `json:"exp"`
}

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (c *hostCredential) signJWT(deviceID, scope string, ttl time.Duration) (string, error) {
	header := b64u([]byte(`{"alg":"EdDSA","typ":"JWT"}`))
	payload, err := json.Marshal(jwtClaims{DeviceID: deviceID, Scope: scope, Exp: time.Now().Add(ttl).Unix()})
	if err != nil {
		return "", err
	}
	signingInput := header + "." + b64u(payload)
	sig := ed25519.Sign(c.privateKey(), []byte(signingInput))
	return signingInput + "." + b64u(sig), nil
}

func (c *hostCredential) verifyJWT(token string) (*jwtClaims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed token")
	}
	header, payload, sigB64 := parts[0], parts[1], parts[2]
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(ed25519.PublicKey(c.PublicKey), []byte(header+"."+payload), sig) {
		return nil, fmt.Errorf("bad signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}
	var claims jwtClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, err
	}
	if time.Now().Unix() > claims.Exp {
		return nil, fmt.Errorf("token expired")
	}
	return &claims, nil
}

func newHostCredential() (*hostCredential, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	caKeyPEM, caCertPEM, err := generateCA()
	if err != nil {
		return nil, err
	}
	return &hostCredential{
		Seed:      priv.Seed(),
		PublicKey: []byte(pub),
		CACertPEM: caCertPEM,
		CAKeyPEM:  caKeyPEM,
	}, nil
}

func generateCA() ([]byte, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "dsh-desktop CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return keyPEM, certPEM, nil
}

func stateDir() string {
	if d := os.Getenv(stateDirEnv); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".dsh-desktop"
	}
	return filepath.Join(home, ".dsh-desktop")
}

func loadOrCreateCredential(dir string) (*hostCredential, error) {
	path := filepath.Join(dir, "credential.json")
	if data, err := os.ReadFile(path); err == nil {
		var c hostCredential
		if json.Unmarshal(data, &c) == nil && len(c.Seed) == ed25519.SeedSize {
			return &c, nil
		}
	}
	c, err := newHostCredential()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	out, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return nil, err
	}
	return c, nil
}

// deviceRegistry tracks paired devices and persists them as JSON.
type deviceRegistry struct {
	mu      sync.Mutex
	devices map[string]deviceIdentity
	path    string
}

func newDeviceRegistry(path string) *deviceRegistry {
	r := &deviceRegistry{devices: map[string]deviceIdentity{}, path: path}
	if data, err := os.ReadFile(path); err == nil {
		var list []deviceIdentity
		if json.Unmarshal(data, &list) == nil {
			for _, d := range list {
				r.devices[d.DeviceID] = d
			}
		}
	}
	return r
}

func (r *deviceRegistry) register(deviceID, name, pubFP string) deviceIdentity {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	d := deviceIdentity{DeviceID: deviceID, Name: name, PublicKeyFP: pubFP, IssuedAt: now, LastActive: now}
	r.devices[deviceID] = d
	r.saveLocked()
	return d
}

func (r *deviceRegistry) saveLocked() {
	var list []deviceIdentity
	for _, d := range r.devices {
		list = append(list, d)
	}
	data, _ := json.MarshalIndent(list, "", "  ")
	_ = os.MkdirAll(filepath.Dir(r.path), 0o700)
	_ = os.WriteFile(r.path, data, 0o600)
}

func (r *deviceRegistry) list() []deviceIdentity {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := make([]deviceIdentity, 0, len(r.devices))
	for _, d := range r.devices {
		list = append(list, d)
	}
	return list
}

func (r *deviceRegistry) revoke(deviceID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.devices[deviceID]; !ok {
		return false
	}
	delete(r.devices, deviceID)
	r.saveLocked()
	return true
}

func (r *deviceRegistry) exists(deviceID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.devices[deviceID]
	return ok
}

// newDeviceID returns a random device identifier.
func newDeviceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// deviceFingerprint derives a stable fingerprint for a device. A full
// per-device public key (for E2E) arrives in a later ticket; this placeholder
// keeps the registry shape ready for it.
func deviceFingerprint(deviceID string) string {
	sum := sha256.Sum256([]byte(deviceID))
	return hex.EncodeToString(sum[:8])
}

// issueLeafCert signs a short-lived server certificate with the Host CA for the
// LAN listener, and returns its SHA-256 fingerprint (hex) for the phone to pin
// on first use.
func (c *hostCredential) issueLeafCert(ip net.IP) (certPEM, keyPEM []byte, fingerprint string, err error) {
	caBlock, _ := pem.Decode(c.CACertPEM)
	if caBlock == nil {
		return nil, nil, "", fmt.Errorf("bad CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return nil, nil, "", err
	}
	caKeyBlock, _ := pem.Decode(c.CAKeyPEM)
	if caKeyBlock == nil {
		return nil, nil, "", fmt.Errorf("bad CA key PEM")
	}
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	if err != nil {
		return nil, nil, "", err
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "dsh-desktop.local"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"dsh-desktop.local"},
	}
	if ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		return nil, nil, "", err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	sum := sha256.Sum256(der)
	return certPEM, keyPEM, hex.EncodeToString(sum[:]), nil
}
