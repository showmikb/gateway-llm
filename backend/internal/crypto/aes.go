package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

func deriveKey() ([]byte, error) {
	key := os.Getenv("GATEWAY_LLM_ENCRYPTION_KEY")
	if key == "" {
		key = os.Getenv("GATEWAY_LLM_MASTER_KEY")
	}
	if key == "" {
		return nil, fmt.Errorf("GATEWAY_LLM_ENCRYPTION_KEY or GATEWAY_LLM_MASTER_KEY must be set")
	}
	hash := sha256.Sum256([]byte(key))
	return hash[:], nil
}

func Encrypt(plaintext string) ([]byte, error) {
	key, err := deriveKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

func Decrypt(ciphertext []byte) (string, error) {
	key, err := deriveKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

func MaskKey(raw string) string {
	if len(raw) <= 8 {
		return "****"
	}
	return raw[:4] + "****" + raw[len(raw)-4:]
}

func EncryptHex(plaintext string) (string, error) {
	enc, err := Encrypt(plaintext)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(enc), nil
}

func DecryptHex(hexCiphertext string) (string, error) {
	ct, err := hex.DecodeString(hexCiphertext)
	if err != nil {
		return "", fmt.Errorf("decode hex: %w", err)
	}
	return Decrypt(ct)
}
