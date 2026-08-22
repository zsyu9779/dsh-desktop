package main

import (
	"bytes"
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
	"errors"
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
// used to sign device JWTs, plus a CA and the stable LAN leaf certificate.
//
// The leaf certificate is persisted so the LAN TLS fingerprint stays stable
// across restarts (ticket 04): the phone pins it on first use, and a rotating
// fingerprint would silently break every previously paired device.
type hostCredential struct {
	Seed      []byte `json:"seed"`
	PublicKey []byte `json:"publicKey"`
	CACertPEM []byte `json:"caCertPem"`
	CAKeyPEM  []byte `json:"caKeyPem"`
	// LeafCertPEM/LeafKeyPEM are the stable leaf used by the LAN listener.
	// LeafIP records which SAN IP the leaf covers; a changed LAN IP re-issues.
	LeafCertPEM []byte `json:"leafCertPem,omitempty"`
	LeafKeyPEM  []byte `json:"leafKeyPem,omitempty"`
	LeafIP      string `json:"leafIp,omitempty"`
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
	if credential, err := loadExistingHostCredential(dir); err == nil {
		return credential, nil
	}
	c, err := newHostCredential()
	if err != nil {
		return nil, err
	}
	if err := c.save(dir); err != nil {
		return nil, err
	}
	return c, nil
}

// loadExistingHostCredential never rotates identity. Callers proving an
// existing LAN Pairing must fail closed when the original key is unavailable.
func loadExistingHostCredential(dir string) (*hostCredential, error) {
	data, err := os.ReadFile(filepath.Join(dir, "credential.json"))
	if err != nil {
		return nil, err
	}
	var credential hostCredential
	if err := json.Unmarshal(data, &credential); err != nil {
		return nil, err
	}
	if len(credential.Seed) != ed25519.SeedSize || len(credential.PublicKey) != ed25519.PublicKeySize {
		return nil, errors.New("invalid Host LAN credential")
	}
	derived := ed25519.NewKeyFromSeed(credential.Seed).Public().(ed25519.PublicKey)
	if !bytes.Equal(derived, credential.PublicKey) {
		return nil, errors.New("Host LAN credential key mismatch")
	}
	return &credential, nil
}

// save persists the credential (including the stable leaf, ticket 04) to
// <dir>/credential.json.
func (c *hostCredential) save(dir string) error {
	path := filepath.Join(dir, "credential.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	out, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
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

// touch refreshes a device's LastActive (in memory only; persisted on
// register/revoke). It is a no-op for a revoked device.
func (r *deviceRegistry) touch(deviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.devices[deviceID]; ok {
		d.LastActive = time.Now()
		r.devices[deviceID] = d
	}
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
//
// Ticket 04: the leaf is persisted and reused when the SAN IP is unchanged, so
// the fingerprint is stable across enable()/restart cycles. Only a changed
// LAN IP (or a missing/expired persisted leaf) triggers a re-issue.
func (c *hostCredential) issueLeafCert(ip net.IP) (certPEM, keyPEM []byte, fingerprint string, err error) {
	ipString := ""
	if ip != nil {
		ipString = ip.String()
	}
	if c.LeafCertPEM != nil && c.LeafKeyPEM != nil && c.LeafIP == ipString {
		if fp, ok := fingerprintOfLeaf(c.LeafCertPEM); ok {
			// Only reuse a persisted leaf that is still within its validity
			// window; an expired leaf is re-issued like a missing one.
			if block, _ := pem.Decode(c.LeafCertPEM); block != nil {
				if cert, err := x509.ParseCertificate(block.Bytes); err == nil && time.Now().Before(cert.NotAfter) {
					return c.LeafCertPEM, c.LeafKeyPEM, fp, nil
				}
			}
		}
	}

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
	// Persist for the next enable()/restart so the fingerprint is stable.
	c.LeafCertPEM = certPEM
	c.LeafKeyPEM = keyPEM
	c.LeafIP = ipString
	return certPEM, keyPEM, hex.EncodeToString(sum[:]), nil
}

// fingerprintOfLeaf returns the SHA-256 fingerprint of the first certificate
// in the PEM block, and whether a certificate was found.
func fingerprintOfLeaf(certPEM []byte) (string, bool) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", false
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), true
}
