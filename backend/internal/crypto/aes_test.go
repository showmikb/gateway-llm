package crypto

import (
	"os"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	os.Setenv("GATEWAY_LLM_ENCRYPTION_KEY", "test-encryption-key-for-testing")
	defer os.Unsetenv("GATEWAY_LLM_ENCRYPTION_KEY")

	plaintext := "sk-test-openai-key-12345"
	encrypted, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	if len(encrypted) == 0 {
		t.Fatal("encrypted bytes should not be empty")
	}

	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptDecryptHex(t *testing.T) {
	os.Setenv("GATEWAY_LLM_ENCRYPTION_KEY", "another-test-key")
	defer os.Unsetenv("GATEWAY_LLM_ENCRYPTION_KEY")

	plaintext := "anthropic-api-key-xyz"
	hexCt, err := EncryptHex(plaintext)
	if err != nil {
		t.Fatalf("EncryptHex failed: %v", err)
	}
	if hexCt == "" {
		t.Fatal("hex ciphertext should not be empty")
	}

	decrypted, err := DecryptHex(hexCt)
	if err != nil {
		t.Fatalf("DecryptHex failed: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sk-abcdefghijklmnop", "sk-a****mnop"},
		{"short", "****"},
		{"12345678", "****"},
		{"", "****"},
	}
	for _, tt := range tests {
		got := MaskKey(tt.input)
		if got != tt.want {
			t.Errorf("MaskKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEncrypt_NoKey(t *testing.T) {
	os.Unsetenv("GATEWAY_LLM_ENCRYPTION_KEY")
	os.Unsetenv("GATEWAY_LLM_MASTER_KEY")

	_, err := Encrypt("test")
	if err == nil {
		t.Error("expected error when no key is set")
	}
}

func TestDecrypt_ShortCiphertext(t *testing.T) {
	os.Setenv("GATEWAY_LLM_ENCRYPTION_KEY", "test-key")
	defer os.Unsetenv("GATEWAY_LLM_ENCRYPTION_KEY")

	_, err := Decrypt([]byte{1, 2, 3})
	if err == nil {
		t.Error("expected error for short ciphertext")
	}
}
