package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"gorm.io/gorm"
)

// SessionTTL is how long a browser login stays valid.
const SessionTTL = 30 * 24 * time.Hour

// DefaultUsername is the implicit account used when auth is disabled (the
// single-user self-hosted case).
const DefaultUsername = "local"

// EnsureDefaultUser finds or creates the implicit single-user account. Used when
// auth is off so all data has an owner without anyone logging in. The password
// is random and unused (there's no login in this mode); if auth is later enabled,
// reset it with `glockpeek -passwd`.
func (db *DB) EnsureDefaultUser() (*User, error) {
	if u, err := db.UserByName(DefaultUsername); err == nil {
		return u, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	pw, err := randToken(24)
	if err != nil {
		return nil, err
	}
	return db.CreateUser(DefaultUsername, pw)
}

// ErrInvalidCredentials is returned by Authenticate on unknown user or bad
// password (same error for both, to avoid leaking which usernames exist).
var ErrInvalidCredentials = errors.New("invalid username or password")

// randToken returns n bytes of crypto-random data as a URL-safe base64 string.
func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken is the deterministic hash stored for API tokens (they're
// high-entropy random strings, so a fast hash is fine — unlike passwords).
func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// ── Users ───────────────────────────────────────────────

// CreateUser creates an account with an argon2id-hashed password.
func (db *DB) CreateUser(username, password string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, errors.New("username and password are required")
	}
	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return nil, err
	}
	u := &User{Username: username, PasswordHash: hash}
	if err := db.Create(u).Error; err != nil {
		return nil, err
	}
	return u, nil
}

// Authenticate verifies a username/password and returns the user.
func (db *DB) Authenticate(username, password string) (*User, error) {
	var u User
	if err := db.Where("username = ?", strings.TrimSpace(username)).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Do the work anyway to keep timing similar whether or not the user
			// exists, then fail uniformly.
			_, _ = argon2id.ComparePasswordAndHash(password, "$argon2id$v=19$m=65536,t=1,p=2$YWJjZGVmZ2g$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaA")
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	ok, err := argon2id.ComparePasswordAndHash(password, u.PasswordHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}
	return &u, nil
}

// SetPassword updates a user's password.
func (db *DB) SetPassword(username, password string) error {
	if password == "" {
		return errors.New("password is required")
	}
	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		return err
	}
	res := db.Model(&User{}).Where("username = ?", strings.TrimSpace(username)).Update("password_hash", hash)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ── Browser sessions ────────────────────────────────────

// CreateSession issues a new opaque session token for a user.
func (db *DB) CreateSession(userID uint) (string, error) {
	tok, err := randToken(32)
	if err != nil {
		return "", err
	}
	s := Session{Token: tok, UserID: userID, ExpiresAt: time.Now().Add(SessionTTL)}
	if err := db.Create(&s).Error; err != nil {
		return "", err
	}
	return tok, nil
}

// UserBySession returns the user for a valid, unexpired session token.
func (db *DB) UserBySession(token string) (*User, error) {
	if token == "" {
		return nil, ErrInvalidCredentials
	}
	var s Session
	if err := db.Where("token = ?", token).First(&s).Error; err != nil {
		return nil, ErrInvalidCredentials
	}
	if time.Now().After(s.ExpiresAt) {
		db.Delete(&s)
		return nil, ErrInvalidCredentials
	}
	var u User
	if err := db.First(&u, s.UserID).Error; err != nil {
		return nil, ErrInvalidCredentials
	}
	return &u, nil
}

// DeleteSession removes a session (logout).
func (db *DB) DeleteSession(token string) error {
	return db.Where("token = ?", token).Delete(&Session{}).Error
}

// ── API tokens (syncer / ingest) ────────────────────────

// CreateAPIToken mints a bearer token for a user and returns the plaintext
// (shown once — only the hash is stored).
func (db *DB) CreateAPIToken(userID uint, name string) (string, error) {
	tok, err := randToken(32)
	if err != nil {
		return "", err
	}
	row := APIToken{Name: name, TokenHash: hashToken(tok), UserID: userID, CreatedAt: time.Now()}
	if err := db.Create(&row).Error; err != nil {
		return "", err
	}
	return tok, nil
}

// UserByAPIToken returns the user a bearer token belongs to, and stamps
// LastUsedAt.
func (db *DB) UserByAPIToken(token string) (*User, error) {
	if token == "" {
		return nil, ErrInvalidCredentials
	}
	var row APIToken
	if err := db.Where("token_hash = ?", hashToken(token)).First(&row).Error; err != nil {
		return nil, ErrInvalidCredentials
	}
	now := time.Now()
	db.Model(&row).Update("last_used_at", &now)
	var u User
	if err := db.First(&u, row.UserID).Error; err != nil {
		return nil, ErrInvalidCredentials
	}
	return &u, nil
}

// UserByName looks up a user by username (for the admin CLI).
func (db *DB) UserByName(username string) (*User, error) {
	var u User
	if err := db.Where("username = ?", strings.TrimSpace(username)).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}
