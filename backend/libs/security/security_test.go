package security

import (
	"context"
	"encoding/base64"
	"testing"
	"time"
)

func TestLocalKMS(t *testing.T) {
	// Generate a 32-byte key
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	kms, err := NewLocalKMS(key)
	if err != nil {
		t.Fatalf("NewLocalKMS failed: %v", err)
	}

	ctx := context.Background()

	t.Run("GenerateDataKey", func(t *testing.T) {
		dek, err := kms.GenerateDataKey(ctx, "test-key")
		if err != nil {
			t.Fatalf("GenerateDataKey failed: %v", err)
		}

		if len(dek.Plaintext) != 32 {
			t.Errorf("expected 32-byte plaintext, got %d", len(dek.Plaintext))
		}

		if len(dek.EncryptedKey) == 0 {
			t.Error("expected non-empty encrypted key")
		}
	})

	t.Run("EncryptDecrypt", func(t *testing.T) {
		plaintext := []byte("Hello, World!")

		ciphertext, err := kms.Encrypt(ctx, "test-key", plaintext)
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}

		decrypted, err := kms.Decrypt(ctx, "test-key", ciphertext)
		if err != nil {
			t.Fatalf("Decrypt failed: %v", err)
		}

		if string(decrypted) != string(plaintext) {
			t.Errorf("expected %q, got %q", plaintext, decrypted)
		}
	})

	t.Run("DecryptDataKey", func(t *testing.T) {
		dek, err := kms.GenerateDataKey(ctx, "test-key")
		if err != nil {
			t.Fatalf("GenerateDataKey failed: %v", err)
		}

		decrypted, err := kms.DecryptDataKey(ctx, "test-key", dek.EncryptedKey)
		if err != nil {
			t.Fatalf("DecryptDataKey failed: %v", err)
		}

		if string(decrypted) != string(dek.Plaintext) {
			t.Error("decrypted key does not match original")
		}
	})
}

func TestEnvelopeEncryptor(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	kms, _ := NewLocalKMS(key)
	encryptor := NewEnvelopeEncryptor(kms, "default-key")

	ctx := context.Background()

	t.Run("EncryptDecrypt", func(t *testing.T) {
		plaintext := []byte("Sensitive data that needs encryption")
		aad := []byte("additional-authenticated-data")

		encrypted, err := encryptor.Encrypt(ctx, plaintext, aad)
		if err != nil {
			t.Fatalf("Encrypt failed: %v", err)
		}

		decrypted, err := encryptor.Decrypt(ctx, encrypted)
		if err != nil {
			t.Fatalf("Decrypt failed: %v", err)
		}

		if string(decrypted) != string(plaintext) {
			t.Errorf("expected %q, got %q", plaintext, decrypted)
		}
	})

	t.Run("DifferentKeys", func(t *testing.T) {
		plaintext := []byte("Test data")

		encrypted1, _ := encryptor.EncryptWithKeyID(ctx, "key1", plaintext, nil)
		encrypted2, _ := encryptor.EncryptWithKeyID(ctx, "key2", plaintext, nil)

		// Same plaintext should produce different ciphertexts
		if string(encrypted1) == string(encrypted2) {
			t.Error("expected different ciphertexts for different keys")
		}
	})
}

func TestRandomSecretGenerator(t *testing.T) {
	gen := &RandomSecretGenerator{Length: 32}

	ctx := context.Background()

	secret1, err := gen.Generate(ctx)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(secret1) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(secret1))
	}

	secret2, _ := gen.Generate(ctx)

	if string(secret1) == string(secret2) {
		t.Error("expected different secrets")
	}
}

func TestBase64SecretGenerator(t *testing.T) {
	gen := &Base64SecretGenerator{ByteLength: 32}

	ctx := context.Background()

	secret, err := gen.Generate(ctx)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should be valid base64
	_, err = base64.StdEncoding.DecodeString(string(secret))
	if err != nil {
		t.Errorf("invalid base64: %v", err)
	}
}

func TestOIDCConfig(t *testing.T) {
	cfg := OIDCConfig{
		ProviderID:   "google",
		Issuer:       "https://accounts.google.com",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURI:  "https://app.example.com/callback",
	}

	provider := NewOIDCProvider(cfg)

	t.Run("DiscoveryURL", func(t *testing.T) {
		if provider.config.DiscoveryURL != "https://accounts.google.com/.well-known/openid-configuration" {
			t.Errorf("unexpected discovery URL: %s", provider.config.DiscoveryURL)
		}
	})

	t.Run("DefaultScopes", func(t *testing.T) {
		if len(provider.config.Scopes) != 3 {
			t.Errorf("expected 3 default scopes, got %d", len(provider.config.Scopes))
		}
	})
}

func TestGenerateState(t *testing.T) {
	state1, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState failed: %v", err)
	}

	if state1 == "" {
		t.Error("expected non-empty state")
	}

	state2, _ := GenerateState()
	if state1 == state2 {
		t.Error("expected different states")
	}
}

func TestGenerateNonce(t *testing.T) {
	nonce1, err := GenerateNonce()
	if err != nil {
		t.Fatalf("GenerateNonce failed: %v", err)
	}

	if nonce1 == "" {
		t.Error("expected non-empty nonce")
	}

	nonce2, _ := GenerateNonce()
	if nonce1 == nonce2 {
		t.Error("expected different nonces")
	}
}

func TestOIDCProviderRegistry(t *testing.T) {
	registry := NewOIDCProviderRegistry()

	provider := NewOIDCProvider(OIDCConfig{
		ProviderID: "test",
		Issuer:     "https://test.example.com",
	})

	registry.Register(provider)

	t.Run("Get", func(t *testing.T) {
		p, ok := registry.Get("test")
		if !ok {
			t.Error("expected to find provider")
		}
		if p.config.ProviderID != "test" {
			t.Errorf("unexpected provider ID: %s", p.config.ProviderID)
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		_, ok := registry.Get("nonexistent")
		if ok {
			t.Error("expected not to find provider")
		}
	})

	t.Run("List", func(t *testing.T) {
		ids := registry.List()
		if len(ids) != 1 {
			t.Errorf("expected 1 provider, got %d", len(ids))
		}
	})
}

func TestCommonOIDCProviders(t *testing.T) {
	providers := CommonOIDCProviders()

	expectedProviders := []string{"google", "microsoft", "okta", "auth0"}
	for _, name := range expectedProviders {
		if _, ok := providers[name]; !ok {
			t.Errorf("missing provider: %s", name)
		}
	}
}

func TestSAMLProvider(t *testing.T) {
	cfg := SAMLConfig{
		ProviderID:                  "test-saml",
		EntityID:                    "https://sp.example.com",
		AssertionConsumerServiceURL: "https://sp.example.com/saml/acs",
		IDPSSOURL:                   "https://idp.example.com/sso",
	}

	provider := NewSAMLProvider(cfg)

	t.Run("GenerateAuthnRequest", func(t *testing.T) {
		req, err := provider.GenerateAuthnRequest("_test-request-id")
		if err != nil {
			t.Fatalf("GenerateAuthnRequest failed: %v", err)
		}

		if req.ID != "_test-request-id" {
			t.Errorf("unexpected ID: %s", req.ID)
		}
		if req.Version != "2.0" {
			t.Errorf("unexpected version: %s", req.Version)
		}
		if req.Issuer.Value != cfg.EntityID {
			t.Errorf("unexpected issuer: %s", req.Issuer.Value)
		}
	})

	t.Run("GenerateSPMetadata", func(t *testing.T) {
		metadata, err := provider.GenerateSPMetadata()
		if err != nil {
			t.Fatalf("GenerateSPMetadata failed: %v", err)
		}

		if metadata == "" {
			t.Error("expected non-empty metadata")
		}
	})
}

func TestGenerateRequestID(t *testing.T) {
	id1, err := GenerateRequestID()
	if err != nil {
		t.Fatalf("GenerateRequestID failed: %v", err)
	}

	if !hasPrefix(id1, "_") {
		t.Errorf("expected ID to start with underscore: %s", id1)
	}

	id2, _ := GenerateRequestID()
	if id1 == id2 {
		t.Error("expected different IDs")
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestDefaultSAMLAttributeMap(t *testing.T) {
	m := DefaultSAMLAttributeMap()

	if m.Email == "" {
		t.Error("expected email attribute")
	}
	if m.FirstName == "" {
		t.Error("expected firstName attribute")
	}
	if m.LastName == "" {
		t.Error("expected lastName attribute")
	}
}

func TestSCIMUser(t *testing.T) {
	user := SCIMUser{
		Schemas:  []string{SCIMSchemaUser},
		ID:       "test-user-id",
		UserName: "testuser",
		Name: &SCIMName{
			GivenName:  "Test",
			FamilyName: "User",
		},
		Emails: []SCIMEmail{
			{Value: "test@example.com", Primary: true},
		},
		Active: true,
	}

	if user.ID != "test-user-id" {
		t.Errorf("unexpected ID: %s", user.ID)
	}
	if user.UserName != "testuser" {
		t.Errorf("unexpected username: %s", user.UserName)
	}
	if user.Name.GivenName != "Test" {
		t.Errorf("unexpected given name: %s", user.Name.GivenName)
	}
}

func TestSCIMGroup(t *testing.T) {
	group := SCIMGroup{
		Schemas:     []string{SCIMSchemaGroup},
		ID:          "test-group-id",
		DisplayName: "Developers",
		Members: []SCIMMember{
			{Value: "user-1"},
			{Value: "user-2"},
		},
	}

	if group.ID != "test-group-id" {
		t.Errorf("unexpected ID: %s", group.ID)
	}
	if len(group.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(group.Members))
	}
}

func TestSCIMSchemas(t *testing.T) {
	if SCIMSchemaUser != "urn:ietf:params:scim:schemas:core:2.0:User" {
		t.Error("invalid user schema")
	}
	if SCIMSchemaGroup != "urn:ietf:params:scim:schemas:core:2.0:Group" {
		t.Error("invalid group schema")
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1, err := GenerateSessionID()
	if err != nil {
		t.Fatalf("GenerateSessionID failed: %v", err)
	}

	if id1 == "" {
		t.Error("expected non-empty session ID")
	}

	id2, _ := GenerateSessionID()
	if id1 == id2 {
		t.Error("expected different session IDs")
	}
}

func TestSession(t *testing.T) {
	now := time.Now()
	session := Session{
		ID:        "test-session",
		UserID:    "test-user",
		OrgID:     "test-org",
		CreatedAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
	}

	if session.ID != "test-session" {
		t.Errorf("unexpected ID: %s", session.ID)
	}
	if session.UserID != "test-user" {
		t.Errorf("unexpected user ID: %s", session.UserID)
	}
}

func TestRefreshToken(t *testing.T) {
	now := time.Now()
	token := RefreshToken{
		ID:        "test-token",
		UserID:    "test-user",
		FamilyID:  "family-1",
		Revoked:   false,
		CreatedAt: now,
		ExpiresAt: now.Add(30 * 24 * time.Hour),
	}

	if token.ID != "test-token" {
		t.Errorf("unexpected ID: %s", token.ID)
	}
	if token.Revoked {
		t.Error("expected token not to be revoked")
	}
}

func TestBuildPermission(t *testing.T) {
	perm := BuildPermission("organization", "read")
	if perm != "organization:read" {
		t.Errorf("unexpected permission: %s", perm)
	}
}

func TestRole_HasPermission(t *testing.T) {
	role := &Role{
		Permissions: []Permission{
			PermOrgRead,
			PermProjectCreate,
			Permission("deployment:*"),
		},
	}

	t.Run("DirectMatch", func(t *testing.T) {
		if !role.HasPermission(PermOrgRead) {
			t.Error("expected role to have organization:read")
		}
	})

	t.Run("NoMatch", func(t *testing.T) {
		if role.HasPermission(PermOrgDelete) {
			t.Error("expected role not to have organization:delete")
		}
	})

	t.Run("WildcardMatch", func(t *testing.T) {
		if !role.HasPermission(PermDeploymentCreate) {
			t.Error("expected role to have deployment:create via wildcard")
		}
	})
}

func TestSystemRoles(t *testing.T) {
	roles := SystemRoles()

	expectedRoles := []string{"owner", "admin", "developer", "viewer", "auditor", "billing"}
	for _, name := range expectedRoles {
		if _, ok := roles[name]; !ok {
			t.Errorf("missing system role: %s", name)
		}
	}

	// Owner should have all permissions
	owner := roles["owner"]
	if !owner.HasPermission(PermOrgManage) {
		t.Error("owner should have org:manage")
	}

	// Viewer should not have write permissions
	viewer := roles["viewer"]
	if viewer.HasPermission(PermDeploymentDeploy) {
		t.Error("viewer should not have deployment:deploy")
	}
}

func TestPermissionGroups(t *testing.T) {
	groups := GetPermissionGroups()

	if len(groups) == 0 {
		t.Error("expected permission groups")
	}

	// Check that each group has permissions
	for _, group := range groups {
		if group.Name == "" {
			t.Error("expected group name")
		}
		if len(group.Permissions) == 0 {
			t.Errorf("expected permissions in group %s", group.Name)
		}
	}
}

func TestKeyCache(t *testing.T) {
	cache := newPermissionCache(time.Second, 10)

	// Test set and get
	cache.set("key1", true)
	allowed, found := cache.get("key1")
	if !found || !allowed {
		t.Error("expected to find key1 with allowed=true")
	}

	// Test not found
	_, found = cache.get("nonexistent")
	if found {
		t.Error("expected not to find nonexistent key")
	}

	// Test expiration
	time.Sleep(1100 * time.Millisecond)
	_, found = cache.get("key1")
	if found {
		t.Error("expected key to be expired")
	}
}

func TestZeroBytes(t *testing.T) {
	data := []byte("sensitive data")
	zeroBytes(data)

	for i, b := range data {
		if b != 0 {
			t.Errorf("byte %d not zeroed: %d", i, b)
		}
	}
}
