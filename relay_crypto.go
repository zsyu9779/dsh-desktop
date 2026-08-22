package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

const relayV1 = "dsh-relay-v1"

var errRelayCiphertext = errors.New("Relay ciphertext authentication failed")

// appendLengthPrefixedField 追加一个长度前缀字段（4 字节大端长度 + 原始字节）。
// relay-v1 握手上下文与 notification-v1 envelope 上下文共用此编码。
func appendLengthPrefixedField(context []byte, field []byte) []byte {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(field)))
	context = append(context, size[:]...)
	return append(context, field...)
}

func relayHandshakeContext(pairingID, hostID, deviceID string, devicePublicKey, hostPublicKey []byte) []byte {
	context := append([]byte(nil), []byte(relayV1+"\x00")...)
	for _, field := range [][]byte{[]byte(pairingID), []byte(hostID), []byte(deviceID), devicePublicKey, hostPublicKey} {
		context = appendLengthPrefixedField(context, field)
	}
	return context
}

func relayPublicKey(privateKey []byte) ([]byte, error) {
	if len(privateKey) != curve25519.ScalarSize {
		return nil, errors.New("invalid X25519 private key")
	}
	return curve25519.X25519(privateKey, curve25519.Basepoint)
}

func relaySharedSecret(privateKey, publicKey []byte) ([]byte, error) {
	if len(privateKey) != curve25519.ScalarSize || len(publicKey) != curve25519.PointSize {
		return nil, errors.New("invalid X25519 identity key")
	}
	return curve25519.X25519(privateKey, publicKey)
}

func relayDeriveKey(material, salt []byte, info string) []byte {
	reader := hkdf.New(sha256.New, material, salt, []byte(info))
	key := make([]byte, 32)
	_, _ = io.ReadFull(reader, key)
	return key
}

func relaySeal(plaintext, key, aad []byte) ([]byte, error) {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return relaySealWithNonce(plaintext, key, aad, nonce)
}

func relaySealWithNonce(plaintext, key, aad, nonce []byte) ([]byte, error) {
	aead, err := relayAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid Relay nonce")
	}
	combined := append([]byte(nil), nonce...)
	return aead.Seal(combined, nonce, plaintext, aad), nil
}

func relayOpen(combined, key, aad []byte) ([]byte, error) {
	aead, err := relayAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(combined) < aead.NonceSize()+aead.Overhead() {
		return nil, errRelayCiphertext
	}
	nonce := combined[:aead.NonceSize()]
	plaintext, err := aead.Open(nil, nonce, combined[aead.NonceSize():], aad)
	if err != nil {
		return nil, errRelayCiphertext
	}
	return plaintext, nil
}

func relayAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
