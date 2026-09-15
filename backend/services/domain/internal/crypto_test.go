package domain

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func testCertificateEncryptor(t *testing.T) *CertificateEncryptor {
	t.Helper()

	key := bytes.Repeat([]byte{0x42}, certificateEncryptionKeySize)
	encryptor, err := NewCertificateEncryptorFromBytes(key)
	if err != nil {
		t.Fatalf("NewCertificateEncryptorFromBytes() error = %v", err)
	}
	return encryptor
}

func TestCertificateEncryptorRoundTrip(t *testing.T) {
	t.Parallel()

	payloads := map[string][]byte{
		"empty": nil,
		"certificate": []byte(`-----BEGIN CERTIFICATE-----
MIIBexamplecertificatepayload
-----END CERTIFICATE-----`),
		"private key": []byte(`-----BEGIN PRIVATE KEY-----
MIIEexampleprivatekeypayload
-----END PRIVATE KEY-----`),
		"large": bytes.Repeat([]byte("certificate-data-"), 64*1024),
	}

	for name, plaintext := range payloads {
		plaintext := plaintext
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			encryptor := testCertificateEncryptor(t)
			ciphertext, err := encryptor.Encrypt(plaintext)
			if err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}

			decrypted, err := encryptor.Decrypt(ciphertext)
			if err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}
			if !bytes.Equal(decrypted, plaintext) {
				t.Fatalf("Decrypt(Encrypt(plaintext)) differs from plaintext")
			}
		})
	}
}

func TestCertificateEncryptorUsesUniqueNonces(t *testing.T) {
	t.Parallel()

	encryptor := testCertificateEncryptor(t)
	plaintext := []byte("same certificate payload")

	first, err := encryptor.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("first Encrypt() error = %v", err)
	}
	second, err := encryptor.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("second Encrypt() error = %v", err)
	}

	if bytes.Equal(first, second) {
		t.Fatal("Encrypt() produced identical ciphertext for the same plaintext")
	}
}

func TestCertificateEncryptorRejectsTamperedCiphertext(t *testing.T) {
	t.Parallel()

	encryptor := testCertificateEncryptor(t)
	ciphertext, err := encryptor.Encrypt([]byte("certificate payload"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	ciphertext[len(ciphertext)-1] ^= 0xff

	_, err = encryptor.Decrypt(ciphertext)
	if !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("Decrypt() error = %v, want ErrDecryptionFailed", err)
	}
}

func TestCertificateEncryptorRejectsWrongKey(t *testing.T) {
	t.Parallel()

	first := testCertificateEncryptor(t)
	second, err := NewCertificateEncryptorFromBytes(bytes.Repeat([]byte{0x24}, certificateEncryptionKeySize))
	if err != nil {
		t.Fatalf("NewCertificateEncryptorFromBytes() error = %v", err)
	}

	ciphertext, err := first.Encrypt([]byte("private key payload"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	_, err = second.Decrypt(ciphertext)
	if !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("Decrypt() error = %v, want ErrDecryptionFailed", err)
	}
}

func TestCertificateEncryptorRejectsTruncatedCiphertext(t *testing.T) {
	t.Parallel()

	encryptor := testCertificateEncryptor(t)
	_, err := encryptor.Decrypt(make([]byte, certificateNonceSize-1))
	if !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("Decrypt() error = %v, want ErrInvalidCiphertext", err)
	}
}

func TestNewCertificateEncryptorRejectsInvalidKeys(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"empty":          "",
		"wrong length":   base64.StdEncoding.EncodeToString(make([]byte, certificateEncryptionKeySize-1)),
		"invalid base64": "not-valid-base64%%%",
	}

	for name, key := range tests {
		key := key
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewCertificateEncryptor(key)
			if !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("NewCertificateEncryptor() error = %v, want ErrInvalidKey", err)
			}
		})
	}
}

func TestGenerateCertificateEncryptionKey(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for i := 0; i < 10; i++ {
		key, err := GenerateCertificateEncryptionKey()
		if err != nil {
			t.Fatalf("GenerateCertificateEncryptionKey() error = %v", err)
		}
		if _, exists := seen[key]; exists {
			t.Fatal("GenerateCertificateEncryptionKey() produced a duplicate key")
		}
		seen[key] = struct{}{}

		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			t.Fatalf("generated key is not valid base64: %v", err)
		}
		if len(decoded) != certificateEncryptionKeySize {
			t.Fatalf("generated key length = %d, want %d", len(decoded), certificateEncryptionKeySize)
		}
		encryptor, err := NewCertificateEncryptor(key)
		if err != nil {
			t.Fatalf("generated key is not usable: %v", err)
		}
		ciphertext, err := encryptor.Encrypt([]byte("certificate"))
		if err != nil {
			t.Fatalf("Encrypt() with generated key error = %v", err)
		}
		plaintext, err := encryptor.Decrypt(ciphertext)
		if err != nil {
			t.Fatalf("Decrypt() with generated key error = %v", err)
		}
		if !bytes.Equal(plaintext, []byte("certificate")) {
			t.Fatal("generated key failed encryption round trip")
		}
	}
}

func TestCertificateCiphertextDoesNotContainPlaintext(t *testing.T) {
	t.Parallel()

	encryptor := testCertificateEncryptor(t)
	plaintext := []byte(`-----BEGIN PRIVATE KEY-----
this-private-key-must-never-be-stored-verbatim
-----END PRIVATE KEY-----`)

	ciphertext, err := encryptor.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Contains(ciphertext, plaintext) {
		t.Fatal("ciphertext contains the plaintext verbatim")
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("Encrypt() returned plaintext unchanged")
	}
}
