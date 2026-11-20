package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Tokens issues and verifies HS256 access tokens.
type Tokens struct {
	secret []byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

// NewTokens creates a token issuer.
func NewTokens(secret, issuer string, ttl time.Duration) *Tokens {
	return &Tokens{secret: []byte(secret), issuer: issuer, ttl: ttl, now: time.Now}
}

// AccessClaims are the claims of an access token.
type AccessClaims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

// Issue signs an access token for a session.
func (t *Tokens) Issue(userID, sessionID uuid.UUID) (string, time.Time, error) {
	now := t.now()
	exp := now.Add(t.ttl)
	claims := AccessClaims{
		SessionID: sessionID.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    t.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.NewString(),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, exp, nil
}

// ErrInvalidToken is returned for any unusable token (bad signature, expired,
// wrong issuer, malformed).
var ErrInvalidToken = errors.New("invalid access token")

// Parse verifies a token and returns the user and session IDs.
func (t *Tokens) Parse(raw string) (userID, sessionID uuid.UUID, err error) {
	var claims AccessClaims
	_, err = jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(t.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return uuid.Nil, uuid.Nil, ErrInvalidToken
	}
	userID, err1 := uuid.Parse(claims.Subject)
	sessionID, err2 := uuid.Parse(claims.SessionID)
	if err1 != nil || err2 != nil {
		return uuid.Nil, uuid.Nil, ErrInvalidToken
	}
	return userID, sessionID, nil
}

// NewRefreshToken returns an opaque random token and its SHA-256 hash (only
// the hash is stored).
func NewRefreshToken() (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashRefreshToken(token), nil
}

// HashRefreshToken hashes a refresh token for lookup.
func HashRefreshToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// NewOTP returns a uniformly random 6-digit code.
func NewOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// HashOTP binds the code to the phone and purpose with an HMAC so a database
// leak does not reveal codes and hashes cannot be replayed across phones.
func HashOTP(secret []byte, phone, purpose, code string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(phone + "|" + purpose + "|" + code))
	return m.Sum(nil)
}
