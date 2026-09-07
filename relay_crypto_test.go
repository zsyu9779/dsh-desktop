package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"os"
	"testing"
)

type relayV1Vector struct {
	Version             int    `json:"version"`
	PairingID           string `json:"pairingID"`
	HostID              string `json:"hostID"`
	DeviceID            string `json:"deviceID"`
	DevicePrivateKey    string `json:"devicePrivateKey"`
	DevicePublicKey     string `json:"devicePublicKey"`
	HostPrivateKey      string `json:"hostPrivateKey"`
	HostPublicKey       string `json:"hostPublicKey"`
	StaticSharedSecret  string `json:"staticSharedSecret"`
	EphemeralPrivateKey string `json:"ephemeralPrivateKey"`
	EphemeralPublicKey  string `json:"ephemeralPublicKey"`
	DeviceNonce         string `json:"deviceNonce"`
	HostNonce           string `json:"hostNonce"`
	HandshakeContext    string `json:"handshakeContext"`
	DeviceHelloKey      string `json:"deviceHelloKey"`
	HostHelloKey        string `json:"hostHelloKey"`
	DeviceToHostKey     string `json:"deviceToHostKey"`
	HostToDeviceKey     string `json:"hostToDeviceKey"`
	HelloAAD            string `json:"helloAAD"`
	DeviceToHostAAD     string `json:"deviceToHostAAD"`
	HostToDeviceAAD     string `json:"hostToDeviceAAD"`
	RPCNonce            string `json:"rpcNonce"`
	RPCPlaintext        string `json:"rpcPlaintext"`
	RPCCiphertext       string `json:"rpcCiphertext"`
}

func TestHostRelayCryptographyMatchesIOSRelayV1Vector(t *testing.T) {
	vector := loadRelayVector(t)
	hostPrivate := decodeVectorValue(t, vector.HostPrivateKey)
	devicePublic := decodeVectorValue(t, vector.DevicePublicKey)
	ephPublic := decodeVectorValue(t, vector.EphemeralPublicKey)
	deviceNonce := decodeVectorValue(t, vector.DeviceNonce)
	hostNonce := decodeVectorValue(t, vector.HostNonce)

	context := relayHandshakeContext(vector.PairingID, vector.HostID, vector.DeviceID, devicePublic, decodeVectorValue(t, vector.HostPublicKey))
	if got := base64.StdEncoding.EncodeToString(context); got != vector.HandshakeContext {
		t.Fatalf("handshake context = %q, want %q", got, vector.HandshakeContext)
	}
	staticSecret, err := relaySharedSecret(hostPrivate, devicePublic)
	if err != nil {
		t.Fatal(err)
	}
	if got := base64.StdEncoding.EncodeToString(staticSecret); got != vector.StaticSharedSecret {
		t.Fatalf("static secret = %q, want %q", got, vector.StaticSharedSecret)
	}
	ephSecret, err := relaySharedSecret(hostPrivate, ephPublic)
	if err != nil {
		t.Fatal(err)
	}
	material := append(append([]byte(nil), staticSecret...), ephSecret...)
	assertVectorKey(t, relayDeriveKey(staticSecret, context, "dsh-relay-v1/device-hello"), vector.DeviceHelloKey)
	assertVectorKey(t, relayDeriveKey(material, append(append([]byte(nil), context...), deviceNonce...), "dsh-relay-v1/host-hello"), vector.HostHelloKey)
	connectionMaterial := append(append([]byte(nil), material...), context...)
	salt := append(append([]byte(nil), deviceNonce...), hostNonce...)
	deviceToHost := relayDeriveKey(connectionMaterial, salt, "dsh-relay-v1/device-host")
	hostToDevice := relayDeriveKey(connectionMaterial, salt, "dsh-relay-v1/host-device")
	assertVectorKey(t, deviceToHost, vector.DeviceToHostKey)
	assertVectorKey(t, hostToDevice, vector.HostToDeviceKey)

	plaintext, err := relayOpen(
		decodeVectorValue(t, vector.RPCCiphertext),
		deviceToHost,
		[]byte(vector.DeviceToHostAAD),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := base64.StdEncoding.EncodeToString(plaintext); got != vector.RPCPlaintext {
		t.Fatalf("RPC plaintext = %q, want %q", got, vector.RPCPlaintext)
	}
}

func TestRelayHandshakeContextUsesLengthDelimitedPublicIdentities(t *testing.T) {
	got := relayHandshakeContext("p", "h", "d", []byte{1, 2}, []byte{3, 4})
	wantPrefix := []byte("dsh-relay-v1\x00")
	if string(got[:len(wantPrefix)]) != string(wantPrefix) {
		t.Fatalf("context prefix = %q", got[:len(wantPrefix)])
	}
	offset := len(wantPrefix)
	for _, want := range [][]byte{[]byte("p"), []byte("h"), []byte("d"), {1, 2}, {3, 4}} {
		length := int(binary.BigEndian.Uint32(got[offset : offset+4]))
		offset += 4
		if length != len(want) || string(got[offset:offset+length]) != string(want) {
			t.Fatalf("field at %d = %x, want %x", offset, got[offset:offset+length], want)
		}
		offset += length
	}
}

func loadRelayVector(t *testing.T) relayV1Vector {
	t.Helper()
	raw, err := os.ReadFile("test-vectors/relay-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector relayV1Vector
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	return vector
}

func decodeVectorValue(t *testing.T, encoded string) []byte {
	t.Helper()
	value, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertVectorKey(t *testing.T, got []byte, want string) {
	t.Helper()
	if encoded := base64.StdEncoding.EncodeToString(got); encoded != want {
		t.Fatalf("key = %q, want %q", encoded, want)
	}
}
