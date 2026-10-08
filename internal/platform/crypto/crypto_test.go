package crypto

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/DueGin/token-market/internal/platform/config"
)

func key(ch string) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(ch, 32)))
}

func TestSealOpenRoundTrip(t *testing.T) {
	kr, err := NewKeyring(config.CryptoConfig{ActiveKeyID: "k1", Keys: map[string]string{"k1": key("a")}})
	if err != nil {
		t.Fatal(err)
	}
	ct, err := kr.Seal([]byte("secret"), []byte("upstream:1"))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := kr.Open(ct, []byte("upstream:1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(pt) != "secret" {
		t.Fatalf("got %q", pt)
	}
}

func TestOpenRejectsWrongAAD(t *testing.T) {
	kr, _ := NewKeyring(config.CryptoConfig{ActiveKeyID: "k1", Keys: map[string]string{"k1": key("a")}})
	ct, _ := kr.Seal([]byte("secret"), []byte("upstream:1"))
	if _, err := kr.Open(ct, []byte("upstream:2")); err == nil {
		t.Fatal("expected failure when ciphertext is moved to another context")
	}
}

func TestRotationKeepsOldCiphertextReadable(t *testing.T) {
	old, _ := NewKeyring(config.CryptoConfig{ActiveKeyID: "k1", Keys: map[string]string{"k1": key("a")}})
	ct, _ := old.Seal([]byte("secret"), nil)

	rotated, err := NewKeyring(config.CryptoConfig{ActiveKeyID: "k2", Keys: map[string]string{"k1": key("a"), "k2": key("b")}})
	if err != nil {
		t.Fatal(err)
	}
	pt, err := rotated.Open(ct, nil)
	if err != nil || string(pt) != "secret" {
		t.Fatalf("old ciphertext unreadable after rotation: %v", err)
	}
	fresh, _ := rotated.Seal([]byte("secret"), nil)
	if !bytes.Contains(fresh[:4], []byte("k2")) {
		t.Fatalf("new ciphertext should use active key k2")
	}
}

func TestOpenRejectsMalformed(t *testing.T) {
	kr, _ := NewKeyring(config.CryptoConfig{ActiveKeyID: "k1", Keys: map[string]string{"k1": key("a")}})
	for _, ct := range [][]byte{nil, {9}, {formatVersion, 5, 'k'}, {formatVersion, 2, 'k', '1', 0, 1}} {
		if _, err := kr.Open(ct, nil); err == nil {
			t.Fatalf("expected error for %v", ct)
		}
	}
}
