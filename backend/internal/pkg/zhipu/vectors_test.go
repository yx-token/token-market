//go:build unit

package zhipu

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type originalVector struct {
	Description string `json:"description"`
	Input       struct {
		Credential    string          `json:"credential"`
		ID            string          `json:"apiKeyId"`
		Secret        string          `json:"apiKeySecret"`
		TS            json.RawMessage `json:"ts"`
		Nonce         string          `json:"nonce"`
		Session       string          `json:"sessionId"`
		Version       string          `json:"clientVersion"`
		AppID         string          `json:"appId"`
		Salt          string          `json:"salt"`
		OutputBytes   int             `json:"output_bytes"`
		PoWBits       int             `json:"powBits"`
		Prefix        string          `json:"random_prefix_hex"`
		Seed          string          `json:"private_key_seed_hex"`
		IV            string          `json:"iv_hex"`
		AAD           string          `json:"additional_data"`
		TagBits       int             `json:"aes_gcm_tag_bits"`
		PKCS8Prefix   string          `json:"pkcs8_prefix_hex"`
		PrivateCipher string          `json:"private_cipher_base64"`
	} `json:"input"`
	Expected json.RawMessage `json:"expected"`
}

type originalExpected struct {
	ID                 string `json:"apiKeyId"`
	Secret             string `json:"apiKeySecret"`
	Credential         string `json:"credential"`
	HMACKey            string `json:"getSignKey_hmac_hex"`
	AESKey             string `json:"ed25519_priv_hex"`
	Message            string `json:"message"`
	Signature          string `json:"sig_base64"`
	SignatureHex       string `json:"sig_hex"`
	PrivateCipher      string `json:"private_cipher_base64"`
	PKCS8              string `json:"pkcs8_base64"`
	PublicKey          string `json:"public_key_hex"`
	DecryptedPKCS8     string `json:"decrypted_pkcs8_base64"`
	DecryptedPublicKey string `json:"decrypted_public_key_hex"`
	ProofMessage       string `json:"decryption_proof_message"`
	ProofSignature     string `json:"decryption_proof_sig_base64"`
	ClientSignature    string `json:"x_client_sig_base64"`
	ClientSignatureHex string `json:"x_client_sig_hex"`
	PoWSeed            string `json:"seed_hex"`
	Candidate          string `json:"candidate"`
	CandidateHash      string `json:"candidate_sha256_hex"`
	Error              string `json:"error"`
}

// The research worker owns this fixture. Every original-ZCode vector must pass.
func TestZCodeSigningVectors(t *testing.T) {
	b, err := os.ReadFile("testdata/zcode_signing_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SchemaVersion int `json:"schema_version"`
		Shared        struct {
			Secret string `json:"apiKeySecret"`
		} `json:"shared_fixture"`
		Vectors map[string][]originalVector `json:"vectors"`
	}
	if err := json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SchemaVersion != 1 {
		t.Fatal("unsupported external signing vector schema")
	}
	for _, group := range []string{"parse_client_signing_credential", "hkdf", "handshake_signature", "private_cipher", "business_signature", "proof_of_work"} {
		vectors := fixture.Vectors[group]
		if len(vectors) == 0 {
			t.Fatalf("missing external vector group %s", group)
		}
		for _, v := range vectors {
			t.Run(group+"/"+v.Description, func(t *testing.T) {
				var want *originalExpected
				if err := json.Unmarshal(v.Expected, &want); err != nil {
					t.Fatal(err)
				}
				check := func(name, got, expected string) {
					t.Helper()
					if got != expected {
						t.Errorf("%s mismatch: got %q want %q", name, got, expected)
					}
				}
				if group == "parse_client_signing_credential" {
					id, secret, err := SplitCredential(v.Input.Credential)
					if want == nil {
						if err == nil {
							t.Fatal("invalid original credential accepted")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					check("apiKeyId", id, want.ID)
					check("apiKeySecret", secret, want.Secret)
					check("credential", id+"."+secret, want.Credential)
					return
				}
				if want == nil {
					t.Fatal("missing expected values")
				}
				ts := strings.Trim(string(v.Input.TS), "\"")
				switch group {
				case "hkdf":
					if v.Input.Salt != SigningKDFSalt || v.Input.OutputBytes != 32 {
						t.Fatal("HKDF parameters differ from original")
					}
					for info, expected := range map[string]string{"getSignKey_hmac": want.HMACKey, "ed25519_priv": want.AESKey} {
						key, err := deriveBytes(v.Input.Secret, info)
						if err != nil {
							t.Fatal(err)
						}
						check(info, hex.EncodeToString(key), expected)
					}
				case "handshake_signature":
					sig, err := handshakeSignature(v.Input.ID, v.Input.Secret, ts, v.Input.Nonce)
					if err != nil {
						t.Fatal(err)
					}
					check("handshake message", "get_sign_key\n"+v.Input.ID+"\n"+ts+"\n"+v.Input.Nonce, want.Message)
					check("HMAC base64", sig, want.Signature)
					decoded, err := base64.StdEncoding.DecodeString(sig)
					if err != nil {
						t.Fatal(err)
					}
					check("HMAC hex", hex.EncodeToString(decoded), want.SignatureHex)
				case "private_cipher":
					private, err := decryptPrivateKey(want.PrivateCipher, v.Input.ID, v.Input.Secret)
					if err != nil {
						t.Fatal(err)
					}
					der, err := x509.MarshalPKCS8PrivateKey(private)
					if err != nil {
						t.Fatal(err)
					}
					encoded := base64.StdEncoding.EncodeToString(der)
					check("PKCS8", encoded, want.PKCS8)
					check("decrypted PKCS8", encoded, want.DecryptedPKCS8)
					if !strings.HasPrefix(hex.EncodeToString(der), v.Input.PKCS8Prefix) {
						t.Error("PKCS8 prefix differs")
					}
					public := hex.EncodeToString(private[32:])
					check("public key", public, want.PublicKey)
					check("decrypted public key", public, want.DecryptedPublicKey)
					seed, err := hex.DecodeString(v.Input.Seed)
					if err != nil || len(seed) != ed25519.SeedSize {
						t.Fatal("invalid external seed")
					}
					if !private.Equal(ed25519.NewKeyFromSeed(seed)) {
						t.Error("decrypted key differs from fixture seed")
					}
					if v.Input.TagBits != 128 || v.Input.AAD != v.Input.ID {
						t.Fatal("AES-GCM parameters differ")
					}
					key, err := deriveBytes(v.Input.Secret, "ed25519_priv")
					if err != nil {
						t.Fatal(err)
					}
					block, err := aes.NewCipher(key)
					if err != nil {
						t.Fatal(err)
					}
					gcm, err := cipher.NewGCM(block)
					if err != nil {
						t.Fatal(err)
					}
					iv, err := hex.DecodeString(v.Input.IV)
					if err != nil || len(iv) != gcm.NonceSize() {
						t.Fatal("invalid fixture IV")
					}
					encrypted := append(iv, gcm.Seal(nil, iv, []byte(encoded), []byte(v.Input.AAD))...)
					check("AES-GCM ciphertext", base64.StdEncoding.EncodeToString(encrypted), want.PrivateCipher)
					check("decryption proof", base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte(want.ProofMessage))), want.ProofSignature)
				case "business_signature":
					private, err := decryptPrivateKey(v.Input.PrivateCipher, v.Input.ID, fixture.Shared.Secret)
					if err != nil {
						t.Fatal(err)
					}
					message := signatureMessage(v.Input.ID, ts, v.Input.Version, v.Input.Session, v.Input.Nonce)
					check("business message", message, want.Message)
					check("public key", hex.EncodeToString(private[32:]), want.PublicKey)
					sig := ed25519.Sign(private, []byte(message))
					check("Ed25519 base64", base64.StdEncoding.EncodeToString(sig), want.ClientSignature)
					check("Ed25519 hex", hex.EncodeToString(sig), want.ClientSignatureHex)
				case "proof_of_work":
					if v.Input.AppID != SigningAppID {
						t.Fatal("PoW appId differs from production")
					}
					seed := powSeed(v.Input.ID, v.Input.Session, ts)
					candidate, err := computePoWBits(context.Background(), seed, v.Input.Prefix, v.Input.PoWBits)
					if want.Error != "" {
						if err == nil {
							t.Fatal("invalid difficulty accepted")
						}
						check("PoW error", err.Error(), want.Error)
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					check("PoW seed", seed, want.PoWSeed)
					check("PoW candidate", candidate, want.Candidate)
					digest := sha256.Sum256([]byte(seed + "\n" + candidate))
					check("PoW SHA256", hex.EncodeToString(digest[:]), want.CandidateHash)
				}
			})
		}
	}
}
