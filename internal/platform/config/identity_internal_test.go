package config

import (
	"bytes"
	"crypto/ed25519"
	"testing"
)

func TestIdentityPrivateKeyFromSeedClearsSeed(t *testing.T) {
	seed := bytes.Repeat([]byte{7}, ed25519.SeedSize)

	privateKey := identityPrivateKeyFromSeed(seed)

	if len(privateKey) != ed25519.PrivateKeySize {
		t.Fatalf("private key length = %d", len(privateKey))
	}
	if !bytes.Equal(seed, make([]byte, ed25519.SeedSize)) {
		t.Fatal("decoded Ed25519 seed was not cleared")
	}
}
