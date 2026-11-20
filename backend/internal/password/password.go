// Package password hashes passwords with Argon2id (RFC 9106) and enforces
// the password policy. Hashes are self-describing PHC strings:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<salt b64>$<key b64>
//
// so parameters can be raised later without breaking stored hashes
// (NeedsRehash reports when to upgrade at the next login).
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Params are Argon2id cost parameters.
type Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
	SaltLen   uint32
	KeyLen    uint32
}

// Default follows the OWASP Password Storage Cheat Sheet minimum for
// Argon2id (19 MiB, 2 iterations, 1 lane).
var Default = Params{MemoryKiB: 19 * 1024, Time: 2, Threads: 1, SaltLen: 16, KeyLen: 32}

// ErrMalformed is returned for hashes that are not Argon2id PHC strings.
var ErrMalformed = errors.New("password: malformed hash")

// Hash derives a new salted Argon2id hash.
func Hash(plain string, p Params) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plain), salt, p.Time, p.MemoryKiB, p.Threads, p.KeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify reports whether plain matches the encoded hash, in constant time.
func Verify(plain, encoded string) (bool, error) {
	p, salt, key, err := decode(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(plain), salt, p.Time, p.MemoryKiB, p.Threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1, nil
}

// NeedsRehash reports whether encoded was made with weaker parameters than p.
func NeedsRehash(encoded string, p Params) bool {
	old, _, _, err := decode(encoded)
	return err != nil || old.MemoryKiB < p.MemoryKiB || old.Time < p.Time
}

// dummy is verified against when an account does not exist, so a login for
// an unknown identifier costs the same time as a wrong password.
var dummy, _ = Hash("not-a-real-password-equiprent", Default)

// VerifyDummy burns the same CPU/memory as a real verification.
func VerifyDummy(plain string) { _, _ = Verify(plain, dummy) }

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrMalformed
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, ErrMalformed
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &p.Threads); err != nil {
		return Params{}, nil, nil, ErrMalformed
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(parts[4])
	key, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(salt) < 8 || len(key) < 16 {
		return Params{}, nil, nil, ErrMalformed
	}
	return p, salt, key, nil
}

// ---- policy ------------------------------------------------------------------------

// MinLength / MaxLength bound passwords (NIST SP 800-63B: length over
// composition rules; the upper bound limits hashing cost).
const (
	MinLength = 10
	MaxLength = 128
)

// common lists frequently breached passwords and patterns that pass the
// length rule. Matching is case-insensitive.
var common = map[string]bool{
	"password123": true, "password1234": true, "1234567890": true, "12345678910": true, "qwertyuiop": true,
	"iloveyou123": true, "welcome@123": true, "welcome1234": true, "admin@1234": true, "administrator": true,
	"passw0rd123": true, "p@ssw0rd123": true, "abcd@123456": true, "india@12345": true, "equiprent123": true,
	"equiprent@123": true, "qwerty12345": true, "letmein1234": true, "1q2w3e4r5t": true, "0987654321": true,
	"1111111111": true, "aaaaaaaaaa": true, "abcdefghij": true, "password@123": true, "india@123456": true,
}

// Check validates a new password against the policy. personal holds values
// the password must not contain (email local part, phone digits, name).
func Check(plain string, personal ...string) error {
	n := utf8.RuneCountInString(plain)
	switch {
	case n < MinLength:
		return fmt.Errorf("must be at least %d characters", MinLength)
	case n > MaxLength:
		return fmt.Errorf("must be at most %d characters", MaxLength)
	case strings.TrimSpace(plain) == "":
		return errors.New("must not be blank")
	}
	lower := strings.ToLower(plain)
	if common[lower] {
		return errors.New("is too common; choose something less guessable")
	}
	distinct := map[rune]bool{}
	for _, r := range lower {
		distinct[r] = true
	}
	if len(distinct) < 4 {
		return errors.New("uses too few different characters")
	}
	letters, others := false, false
	for _, r := range plain {
		if unicode.IsLetter(r) {
			letters = true
		} else {
			others = true
		}
	}
	if !letters || !others {
		return errors.New("must mix letters with digits or symbols")
	}
	for _, v := range personal {
		v = strings.ToLower(strings.TrimSpace(v))
		if len(v) >= 4 && strings.Contains(lower, v) {
			return errors.New("must not contain your name, email or phone number")
		}
	}
	return nil
}
