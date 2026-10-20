package payment

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ReadKeyMaterial 返回配置中的密钥内容：inline 不为空时直接使用，否则读取文件。两者都为空时返回空串。
func ReadKeyMaterial(inline, file string) (string, error) {
	if strings.TrimSpace(inline) != "" {
		return inline, nil
	}
	if file == "" {
		return "", nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// derOf 接受 PEM 或支付宝密钥工具生成的不带头尾的 base64。
func derOf(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "-----BEGIN") {
		block, _ := pem.Decode([]byte(s))
		if block == nil {
			return nil, errors.New("invalid PEM")
		}
		return block.Bytes, nil
	}
	return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
}

// ParsePrivateKey 解析 RSA 私钥（PKCS#8 或 PKCS#1）。
func ParsePrivateKey(s string) (*rsa.PrivateKey, error) {
	der, err := derOf(s)
	if err != nil {
		return nil, fmt.Errorf("private key: %w", err)
	}
	if k, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("private key: not RSA")
		}
		return rk, nil
	}
	k, err := x509.ParsePKCS1PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("private key: %w", err)
	}
	return k, nil
}

// ParsePublicKey 解析 RSA 公钥（PKIX 或 PKCS#1），也接受证书。
func ParsePublicKey(s string) (*rsa.PublicKey, error) {
	if strings.Contains(s, "BEGIN CERTIFICATE") {
		c, err := ParseCertificate(s)
		if err != nil {
			return nil, err
		}
		pk, ok := c.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("certificate key is not RSA")
		}
		return pk, nil
	}
	der, err := derOf(s)
	if err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}
	if k, err := x509.ParsePKIXPublicKey(der); err == nil {
		rk, ok := k.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("public key: not RSA")
		}
		return rk, nil
	}
	k, err := x509.ParsePKCS1PublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("public key: %w", err)
	}
	return k, nil
}

func ParseCertificate(s string) (*x509.Certificate, error) {
	der, err := derOf(s)
	if err != nil {
		return nil, fmt.Errorf("certificate: %w", err)
	}
	return x509.ParseCertificate(der)
}

// SignSHA256 返回 SHA256WithRSA 签名的 base64。
func SignSHA256(key *rsa.PrivateKey, msg string) (string, error) {
	sum := sha256.Sum256([]byte(msg))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// VerifySHA256 校验 SHA256WithRSA 签名（base64）。
func VerifySHA256(key *rsa.PublicKey, msg, sig string) error {
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	sum := sha256.Sum256([]byte(msg))
	return rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], raw)
}

// EncodePrivateKeyPEM 与 EncodePublicKeyPEM 供开发环境与测试生成密钥文件。
func EncodePrivateKeyPEM(k *rsa.PrivateKey) string {
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func EncodePublicKeyPEM(k *rsa.PublicKey) string {
	der, _ := x509.MarshalPKIXPublicKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}
