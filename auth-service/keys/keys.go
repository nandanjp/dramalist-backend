package keys

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

type KeyPair struct {
	Private *rsa.PrivateKey
	Public  *rsa.PublicKey
}

const (
	privateKeyFile = "auth_private.pem"
	publicKeyFile  = "auth_public.pem"
)

// LoadOrGenerate loads an existing RSA key pair from dir, or generates a new
// 2048-bit pair and writes both PEM files on first boot. The public key is
// written to the shared Docker volume so the OpenResty gateway can verify JWTs
// without calling back to auth-service on every request.
func LoadOrGenerate(dir string) (*KeyPair, error) {
	privPath := filepath.Join(dir, privateKeyFile)
	pubPath := filepath.Join(dir, publicKeyFile)

	if fileExists(privPath) && fileExists(pubPath) {
		return load(privPath, pubPath)
	}

	slog.Info("[keys] generating RSA-2048 key pair", "dir", dir)
	return generate(privPath, pubPath)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func load(privPath, pubPath string) (*KeyPair, error) {
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}

	privBlock, _ := pem.Decode(privPEM)
	if privBlock == nil {
		return nil, fmt.Errorf("invalid private key PEM")
	}
	priv, err := x509.ParsePKCS1PrivateKey(privBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	pubBlock, _ := pem.Decode(pubPEM)
	if pubBlock == nil {
		return nil, fmt.Errorf("invalid public key PEM")
	}
	pubAny, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not RSA")
	}

	slog.Info("[keys] RSA key pair loaded from disk")
	return &KeyPair{Private: priv, Public: pub}, nil
}

func generate(privPath, pubPath string) (*KeyPair, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate RSA key: %w", err)
	}

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})
	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(pubPath, pubPEM, 0644); err != nil {
		return nil, fmt.Errorf("write public key: %w", err)
	}

	slog.Info("[keys] RSA key pair generated and written", "private", privPath, "public", pubPath)
	return &KeyPair{Private: priv, Public: &priv.PublicKey}, nil
}
