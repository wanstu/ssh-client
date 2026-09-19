package sshclient

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestKnownHostStoreUnknownMatchChanged(t *testing.T) {
	store := NewKnownHostStore(t.TempDir())
	key1 := testPublicKey(t)
	key2 := testPublicKey(t)
	address := "example.test:22"

	status, previous, err := store.Check(address, key1)
	if err != nil {
		t.Fatal(err)
	}
	if status != "unknown" || previous != nil {
		t.Fatalf("unexpected initial result: %s %#v", status, previous)
	}

	if err := store.Trust(address, key1); err != nil {
		t.Fatal(err)
	}
	status, previous, err = store.Check(address, key1)
	if err != nil {
		t.Fatal(err)
	}
	if status != "match" || previous == nil {
		t.Fatalf("expected match, got %s %#v", status, previous)
	}

	status, previous, err = store.Check(address, key2)
	if err != nil {
		t.Fatal(err)
	}
	if status != "changed" || previous == nil || previous.Fingerprint != ssh.FingerprintSHA256(key1) {
		t.Fatalf("expected changed with previous fingerprint, got %s %#v", status, previous)
	}
}

func testPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}
