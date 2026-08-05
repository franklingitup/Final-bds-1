package security

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionManager manages user sessions with Redis-backed storage.
type SessionManager struct {
	client    redis.UniversalClient
	keyPrefix string
	ttl       time.Duration
}

// Session represents a user session.
type Session struct {
	ID           string            `json:"id"`
	UserID       string            `json:"userId"`
	OrgID        string            `json:"orgId,omitempty"`
	Email        string            `json:"email,omitempty"`
	DeviceInfo   string            `json:"deviceInfo,omitempty"`
	IPAddress    string            `json:"ipAddress,omitempty"`
	UserAgent    string            `json:"userAgent,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	LastActiveAt time.Time         `json:"lastActiveAt"`
	ExpiresAt    time.Time         `json:"expiresAt"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// NewSessionManager creates a new session manager.
func NewSessionManager(client redis.UniversalClient, keyPrefix string, ttl time.Duration) *SessionManager {
	if keyPrefix == "" {
		keyPrefix = "session:"
	}
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	return &SessionManager{
		client:    client,
		keyPrefix: keyPrefix,
		ttl:       ttl,
	}
}

// Create creates a new session.
func (m *SessionManager) Create(ctx context.Context, session *Session) error {
	if session.ID == "" {
		id, err := GenerateSessionID()
		if err != nil {
			return err
		}
		session.ID = id
	}

	now := time.Now()
	session.CreatedAt = now
	session.LastActiveAt = now
	session.ExpiresAt = now.Add(m.ttl)

	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	// Store session
	sessionKey := m.keyPrefix + session.ID
	if err := m.client.Set(ctx, sessionKey, data, m.ttl).Err(); err != nil {
		return err
	}

	// Add to user's session set
	userKey := m.keyPrefix + "user:" + session.UserID
	if err := m.client.SAdd(ctx, userKey, session.ID).Err(); err != nil {
		return err
	}
	m.client.Expire(ctx, userKey, m.ttl*2)

	return nil
}

// Get retrieves a session by ID.
func (m *SessionManager) Get(ctx context.Context, sessionID string) (*Session, error) {
	data, err := m.client.Get(ctx, m.keyPrefix+sessionID).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("session not found")
		}
		return nil, err
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	// Check expiration
	if time.Now().After(session.ExpiresAt) {
		m.Delete(ctx, sessionID)
		return nil, fmt.Errorf("session expired")
	}

	return &session, nil
}

// Touch updates the last active time and extends the session.
func (m *SessionManager) Touch(ctx context.Context, sessionID string) error {
	session, err := m.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	session.LastActiveAt = time.Now()
	session.ExpiresAt = time.Now().Add(m.ttl)

	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	return m.client.Set(ctx, m.keyPrefix+sessionID, data, m.ttl).Err()
}

// Delete removes a session.
func (m *SessionManager) Delete(ctx context.Context, sessionID string) error {
	session, _ := m.Get(ctx, sessionID)

	if err := m.client.Del(ctx, m.keyPrefix+sessionID).Err(); err != nil {
		return err
	}

	// Remove from user's session set
	if session != nil {
		m.client.SRem(ctx, m.keyPrefix+"user:"+session.UserID, sessionID)
	}

	return nil
}

// DeleteAllForUser removes all sessions for a user.
func (m *SessionManager) DeleteAllForUser(ctx context.Context, userID string) (int, error) {
	userKey := m.keyPrefix + "user:" + userID
	sessionIDs, err := m.client.SMembers(ctx, userKey).Result()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, sessionID := range sessionIDs {
		if err := m.Delete(ctx, sessionID); err == nil {
			count++
		}
	}

	m.client.Del(ctx, userKey)
	return count, nil
}

// ListForUser returns all sessions for a user.
func (m *SessionManager) ListForUser(ctx context.Context, userID string) ([]Session, error) {
	userKey := m.keyPrefix + "user:" + userID
	sessionIDs, err := m.client.SMembers(ctx, userKey).Result()
	if err != nil {
		return nil, err
	}

	sessions := make([]Session, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		session, err := m.Get(ctx, sessionID)
		if err != nil {
			// Session expired or deleted, clean up
			m.client.SRem(ctx, userKey, sessionID)
			continue
		}
		sessions = append(sessions, *session)
	}

	return sessions, nil
}

// GenerateSessionID generates a cryptographically secure session ID.
func GenerateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// TokenRevocationList manages revoked tokens.
type TokenRevocationList struct {
	client    redis.UniversalClient
	keyPrefix string
}

// NewTokenRevocationList creates a new revocation list.
func NewTokenRevocationList(client redis.UniversalClient, keyPrefix string) *TokenRevocationList {
	if keyPrefix == "" {
		keyPrefix = "revoked:"
	}
	return &TokenRevocationList{
		client:    client,
		keyPrefix: keyPrefix,
	}
}

// Revoke adds a token to the revocation list.
func (r *TokenRevocationList) Revoke(ctx context.Context, tokenID string, expiresAt time.Time) error {
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return nil // Token already expired, no need to revoke
	}
	return r.client.Set(ctx, r.keyPrefix+tokenID, "1", ttl).Err()
}

// IsRevoked checks if a token is revoked.
func (r *TokenRevocationList) IsRevoked(ctx context.Context, tokenID string) (bool, error) {
	exists, err := r.client.Exists(ctx, r.keyPrefix+tokenID).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// AnyRevoked reports whether any of the given IDs are present in the revocation
// list, in a single Redis round-trip (EXISTS over all keys at once). This lets a
// caller check several related identifiers together - e.g. an access token's JTI
// and the session ID it is bound to - without a round-trip per key. Empty IDs are
// ignored; if no non-empty IDs remain it returns (false, nil) without touching
// Redis.
func (r *TokenRevocationList) AnyRevoked(ctx context.Context, ids ...string) (bool, error) {
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		keys = append(keys, r.keyPrefix+id)
	}
	if len(keys) == 0 {
		return false, nil
	}
	n, err := r.client.Exists(ctx, keys...).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// RevokeAllForUser revokes all tokens for a user.
func (r *TokenRevocationList) RevokeAllForUser(ctx context.Context, userID string, maxAge time.Duration) error {
	// Store user revocation timestamp
	key := r.keyPrefix + "user:" + userID
	return r.client.Set(ctx, key, time.Now().Unix(), maxAge).Err()
}

// IsUserRevoked checks if all tokens for a user are revoked.
func (r *TokenRevocationList) IsUserRevoked(ctx context.Context, userID string, tokenIssuedAt time.Time) (bool, error) {
	key := r.keyPrefix + "user:" + userID
	timestamp, err := r.client.Get(ctx, key).Int64()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	revokedAt := time.Unix(timestamp, 0)
	return tokenIssuedAt.Before(revokedAt), nil
}

// TokenBlacklist provides a memory + Redis backed token blacklist.
type TokenBlacklist struct {
	redis     *TokenRevocationList
	local     map[string]time.Time
	localMu   sync.RWMutex
	syncChan  chan string
	maxLocal  int
}

// NewTokenBlacklist creates a new token blacklist.
func NewTokenBlacklist(redis *TokenRevocationList, maxLocalEntries int) *TokenBlacklist {
	bl := &TokenBlacklist{
		redis:    redis,
		local:    make(map[string]time.Time),
		syncChan: make(chan string, 100),
		maxLocal: maxLocalEntries,
	}
	go bl.cleanupLoop()
	return bl
}

// Add adds a token to the blacklist.
func (b *TokenBlacklist) Add(ctx context.Context, tokenID string, expiresAt time.Time) error {
	// Add to local cache
	b.localMu.Lock()
	b.local[tokenID] = expiresAt
	if len(b.local) > b.maxLocal {
		b.evictOldest()
	}
	b.localMu.Unlock()

	// Add to Redis
	return b.redis.Revoke(ctx, tokenID, expiresAt)
}

// IsBlacklisted checks if a token is blacklisted.
func (b *TokenBlacklist) IsBlacklisted(ctx context.Context, tokenID string) (bool, error) {
	// Check local cache first
	b.localMu.RLock()
	expiresAt, found := b.local[tokenID]
	b.localMu.RUnlock()

	if found {
		if time.Now().After(expiresAt) {
			// Expired, remove from local
			b.localMu.Lock()
			delete(b.local, tokenID)
			b.localMu.Unlock()
			return false, nil
		}
		return true, nil
	}

	// Check Redis
	return b.redis.IsRevoked(ctx, tokenID)
}

func (b *TokenBlacklist) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for k, v := range b.local {
		if oldestKey == "" || v.Before(oldestTime) {
			oldestKey = k
			oldestTime = v
		}
	}

	if oldestKey != "" {
		delete(b.local, oldestKey)
	}
}

func (b *TokenBlacklist) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		b.localMu.Lock()
		now := time.Now()
		for k, v := range b.local {
			if now.After(v) {
				delete(b.local, k)
			}
		}
		b.localMu.Unlock()
	}
}

// RefreshTokenStore manages refresh tokens with rotation.
type RefreshTokenStore struct {
	client    redis.UniversalClient
	keyPrefix string
	ttl       time.Duration
}

// RefreshToken represents a refresh token.
type RefreshToken struct {
	ID           string    `json:"id"`
	UserID       string    `json:"userId"`
	SessionID    string    `json:"sessionId,omitempty"`
	TokenHash    string    `json:"tokenHash"`
	FamilyID     string    `json:"familyId"` // For rotation detection
	Revoked      bool      `json:"revoked"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
	LastUsedAt   time.Time `json:"lastUsedAt,omitempty"`
	IPAddress    string    `json:"ipAddress,omitempty"`
	UserAgent    string    `json:"userAgent,omitempty"`
}

// NewRefreshTokenStore creates a new refresh token store.
func NewRefreshTokenStore(client redis.UniversalClient, keyPrefix string, ttl time.Duration) *RefreshTokenStore {
	if keyPrefix == "" {
		keyPrefix = "refresh:"
	}
	if ttl == 0 {
		ttl = 30 * 24 * time.Hour // 30 days
	}
	return &RefreshTokenStore{
		client:    client,
		keyPrefix: keyPrefix,
		ttl:       ttl,
	}
}

// Store saves a refresh token.
func (s *RefreshTokenStore) Store(ctx context.Context, token *RefreshToken) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}

	// Store token
	tokenKey := s.keyPrefix + token.ID
	if err := s.client.Set(ctx, tokenKey, data, s.ttl).Err(); err != nil {
		return err
	}

	// Add to user's token set
	userKey := s.keyPrefix + "user:" + token.UserID
	if err := s.client.SAdd(ctx, userKey, token.ID).Err(); err != nil {
		return err
	}
	s.client.Expire(ctx, userKey, s.ttl)

	// Track family
	familyKey := s.keyPrefix + "family:" + token.FamilyID
	if err := s.client.SAdd(ctx, familyKey, token.ID).Err(); err != nil {
		return err
	}
	s.client.Expire(ctx, familyKey, s.ttl)

	return nil
}

// Get retrieves a refresh token.
func (s *RefreshTokenStore) Get(ctx context.Context, tokenID string) (*RefreshToken, error) {
	data, err := s.client.Get(ctx, s.keyPrefix+tokenID).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("token not found")
		}
		return nil, err
	}

	var token RefreshToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}

	return &token, nil
}

// Revoke revokes a refresh token.
func (s *RefreshTokenStore) Revoke(ctx context.Context, tokenID string) error {
	token, err := s.Get(ctx, tokenID)
	if err != nil {
		return err
	}

	token.Revoked = true
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}

	return s.client.Set(ctx, s.keyPrefix+tokenID, data, time.Until(token.ExpiresAt)).Err()
}

// RevokeFamily revokes all tokens in a family (used when reuse is detected).
func (s *RefreshTokenStore) RevokeFamily(ctx context.Context, familyID string) error {
	familyKey := s.keyPrefix + "family:" + familyID
	tokenIDs, err := s.client.SMembers(ctx, familyKey).Result()
	if err != nil {
		return err
	}

	for _, tokenID := range tokenIDs {
		s.Revoke(ctx, tokenID)
	}

	return nil
}

// RevokeAllForUser revokes all refresh tokens for a user.
func (s *RefreshTokenStore) RevokeAllForUser(ctx context.Context, userID string) error {
	userKey := s.keyPrefix + "user:" + userID
	tokenIDs, err := s.client.SMembers(ctx, userKey).Result()
	if err != nil {
		return err
	}

	for _, tokenID := range tokenIDs {
		s.Revoke(ctx, tokenID)
	}

	return s.client.Del(ctx, userKey).Err()
}

// Rotate creates a new token and revokes the old one.
func (s *RefreshTokenStore) Rotate(ctx context.Context, oldTokenID string, newToken *RefreshToken) error {
	oldToken, err := s.Get(ctx, oldTokenID)
	if err != nil {
		return err
	}

	if oldToken.Revoked {
		// Reuse detected! Revoke entire family
		s.RevokeFamily(ctx, oldToken.FamilyID)
		return fmt.Errorf("refresh token reuse detected")
	}

	// Inherit family ID
	newToken.FamilyID = oldToken.FamilyID

	// Revoke old token
	if err := s.Revoke(ctx, oldTokenID); err != nil {
		return err
	}

	// Store new token
	return s.Store(ctx, newToken)
}
