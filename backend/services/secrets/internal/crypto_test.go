package secrets

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestEncryptor_EncryptDecrypt(t *testing.T) {
	// Generate a test key.
	key, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty", ""},
		{"simple", "hello world"},
		{"special chars", "postgres://user:p@ss!#$%&*()@host:5432/db"},
		{"unicode", "こんにちは世界 🔐"},
		{"long", string(make([]byte, 10000))},
		{"json", `{"key": "value", "nested": {"a": 1}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := enc.EncryptString(tt.plaintext)
			if err != nil {
				t.Fatalf("encrypt failed: %v", err)
			}

			// Ciphertext should be different from plaintext.
			if bytes.Equal(ciphertext, []byte(tt.plaintext)) && len(tt.plaintext) > 0 {
				t.Error("ciphertext equals plaintext")
			}

			// Decrypt.
			decrypted, err := enc.DecryptString(ciphertext)
			if err != nil {
				t.Fatalf("decrypt failed: %v", err)
			}

			if decrypted != tt.plaintext {
				t.Errorf("got %q, want %q", decrypted, tt.plaintext)
			}
		})
	}
}

func TestEncryptor_DifferentNonces(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)

	plaintext := "same secret value"

	// Encrypt the same value multiple times.
	ciphertext1, _ := enc.EncryptString(plaintext)
	ciphertext2, _ := enc.EncryptString(plaintext)
	ciphertext3, _ := enc.EncryptString(plaintext)

	// Each ciphertext should be different due to unique nonces.
	if bytes.Equal(ciphertext1, ciphertext2) {
		t.Error("ciphertext1 equals ciphertext2 (nonce should be different)")
	}
	if bytes.Equal(ciphertext2, ciphertext3) {
		t.Error("ciphertext2 equals ciphertext3 (nonce should be different)")
	}
	if bytes.Equal(ciphertext1, ciphertext3) {
		t.Error("ciphertext1 equals ciphertext3 (nonce should be different)")
	}

	// But all should decrypt to the same value.
	d1, _ := enc.DecryptString(ciphertext1)
	d2, _ := enc.DecryptString(ciphertext2)
	d3, _ := enc.DecryptString(ciphertext3)

	if d1 != plaintext || d2 != plaintext || d3 != plaintext {
		t.Error("decrypted values don't match original")
	}
}

func TestEncryptor_TamperedCiphertext(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)

	ciphertext, _ := enc.EncryptString("secret value")

	// Tamper with the ciphertext (flip a byte).
	if len(ciphertext) > 20 {
		ciphertext[20] ^= 0xFF
	}

	// Decryption should fail.
	_, err := enc.DecryptString(ciphertext)
	if err == nil {
		t.Error("expected decryption to fail with tampered ciphertext")
	}
}

func TestEncryptor_WrongKey(t *testing.T) {
	key1, _ := GenerateMasterKey()
	key2, _ := GenerateMasterKey()

	enc1, _ := NewEncryptor(key1)
	enc2, _ := NewEncryptor(key2)

	ciphertext, _ := enc1.EncryptString("secret value")

	// Decrypting with the wrong key should fail.
	_, err := enc2.DecryptString(ciphertext)
	if err == nil {
		t.Error("expected decryption to fail with wrong key")
	}
}

func TestEncryptor_InvalidKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"empty", ""},
		{"too short", base64.StdEncoding.EncodeToString(make([]byte, 16))},
		{"too long", base64.StdEncoding.EncodeToString(make([]byte, 64))},
		{"invalid base64", "not-valid-base64!@#$"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEncryptor(tt.key)
			if err == nil {
				t.Error("expected error for invalid key")
			}
		})
	}
}

func TestHashValue(t *testing.T) {
	tests := []struct {
		name      string
		input1    string
		input2    string
		wantEqual bool
	}{
		{"same value", "secret", "secret", true},
		{"different value", "secret1", "secret2", false},
		{"case sensitive", "Secret", "secret", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h1 := HashValue(tt.input1)
			h2 := HashValue(tt.input2)

			if (h1 == h2) != tt.wantEqual {
				t.Errorf("hash equality: got %v, want %v", h1 == h2, tt.wantEqual)
			}

			// Hash should be 64 hex chars (SHA-256).
			if len(h1) != 64 {
				t.Errorf("hash length: got %d, want 64", len(h1))
			}
		})
	}
}

func TestGenerateMasterKey(t *testing.T) {
	keys := make(map[string]bool)

	// Generate several keys.
	for i := 0; i < 10; i++ {
		key, err := GenerateMasterKey()
		if err != nil {
			t.Fatalf("failed to generate key: %v", err)
		}

		// Verify it's valid base64.
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			t.Fatalf("key is not valid base64: %v", err)
		}

		// Verify length is 32 bytes (256 bits).
		if len(decoded) != 32 {
			t.Errorf("decoded key length: got %d, want 32", len(decoded))
		}

		// Verify uniqueness.
		if keys[key] {
			t.Error("duplicate key generated")
		}
		keys[key] = true

		// Verify it can be used to create an encryptor.
		_, err = NewEncryptor(key)
		if err != nil {
			t.Errorf("generated key not usable: %v", err)
		}
	}
}

// SECURITY TEST: Verify plaintext is never stored.
func TestEncryptor_PlaintextNotInCiphertext(t *testing.T) {
	key, _ := GenerateMasterKey()
	enc, _ := NewEncryptor(key)

	secrets := []string{
		"postgres://user:password@host/db",
		"sk_live_abc123",
		"very_secret_api_key",
	}

	for _, secret := range secrets {
		ciphertext, _ := enc.EncryptString(secret)

		// Ensure the plaintext doesn't appear in the ciphertext.
		if bytes.Contains(ciphertext, []byte(secret)) {
			t.Errorf("plaintext %q found in ciphertext", secret)
		}
	}
}
