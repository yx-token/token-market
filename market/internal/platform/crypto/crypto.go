// Package crypto 提供应用层字段加密（AES-256-GCM），用于凭证、密码、证件号等 *_enc 字段。
//
// 密文格式：version(1) | keyIDLen(1) | keyID | nonce(12) | ciphertext+tag。
// 携带 keyID 使得轮换密钥后旧数据仍可解密。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/yx-token/idlicat/market/internal/platform/config"
)

const formatVersion byte = 1

var ErrMalformed = errors.New("crypto: malformed ciphertext")

type Keyring struct {
	activeID string
	aeads    map[string]cipher.AEAD
	pepper   []byte
}

// NewKeyring 由配置构建密钥环。activeID 必须存在于 keys 中。
func NewKeyring(cfg config.CryptoConfig) (*Keyring, error) {
	if cfg.ActiveKeyID == "" {
		return nil, errors.New("crypto: active key id is empty")
	}
	kr := &Keyring{activeID: cfg.ActiveKeyID, aeads: make(map[string]cipher.AEAD, len(cfg.Keys))}
	for id, raw := range cfg.Keys {
		if len(id) == 0 || len(id) > 255 {
			return nil, fmt.Errorf("crypto: invalid key id %q", id)
		}
		key, err := config.DecodeKey(raw)
		if err != nil {
			return nil, fmt.Errorf("crypto: key %q: %w", id, err)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		kr.aeads[id] = aead
	}
	if _, ok := kr.aeads[cfg.ActiveKeyID]; !ok {
		return nil, fmt.Errorf("crypto: active key %q not configured", cfg.ActiveKeyID)
	}
	if cfg.HashPepper != "" {
		kr.pepper = []byte(cfg.HashPepper)
	} else {
		// 未配置时从活动密钥派生；轮换活动密钥会改变盲索引，所以生产环境应显式配置 hash_pepper
		key, _ := config.DecodeKey(cfg.Keys[cfg.ActiveKeyID])
		sum := sha256.Sum256(append([]byte("token-market/blind-index/"), key...))
		kr.pepper = sum[:]
	}
	return kr, nil
}

// BlindIndex 返回不可逆、可比较的哈希，用于在不解密的情况下按敏感值查找（如证件号去重）。
func (k *Keyring) BlindIndex(value string) string {
	m := hmac.New(sha256.New, k.pepper)
	m.Write([]byte(value))
	return hex.EncodeToString(m.Sum(nil))
}

// SealString / OpenString 是字符串的便捷封装。
func (k *Keyring) SealString(s string, aad string) ([]byte, error) {
	return k.Seal([]byte(s), []byte(aad))
}

func (k *Keyring) OpenString(ct []byte, aad string) (string, error) {
	pt, err := k.Open(ct, []byte(aad))
	return string(pt), err
}

// Seal 用当前活动密钥加密。aad 用于绑定上下文（例如 "upstream:42:credentials"），
// 防止密文被挪到其他行或字段使用。
func (k *Keyring) Seal(plaintext, aad []byte) ([]byte, error) {
	aead := k.aeads[k.activeID]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("crypto: nonce: %w", err)
	}
	out := make([]byte, 0, 2+len(k.activeID)+len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, formatVersion, byte(len(k.activeID)))
	out = append(out, k.activeID...)
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, aad), nil
}

// Open 解密 Seal 产生的密文。
func (k *Keyring) Open(ciphertext, aad []byte) ([]byte, error) {
	if len(ciphertext) < 2 || ciphertext[0] != formatVersion {
		return nil, ErrMalformed
	}
	idLen := int(ciphertext[1])
	rest := ciphertext[2:]
	if len(rest) < idLen {
		return nil, ErrMalformed
	}
	id := string(rest[:idLen])
	rest = rest[idLen:]
	aead, ok := k.aeads[id]
	if !ok {
		return nil, fmt.Errorf("crypto: unknown key id %q", id)
	}
	if len(rest) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrMalformed
	}
	nonce, sealed := rest[:aead.NonceSize()], rest[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, fmt.Errorf("crypto: open: %w", err)
	}
	return plaintext, nil
}
