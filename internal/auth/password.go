package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrInvalidPasswordHash = errors.New("invalid password hash")
)

const (
	passwordHashMemory      uint32 = 19 * 1024
	passwordHashIterations  uint32 = 2
	passwordHashParallelism uint8  = 1
	passwordHashSaltLength         = 16
	passwordHashKeyLength          = 32

	maxPHCLength       = 512
	maxSaltEncodedLen  = 44
	maxHashEncodedLen  = 88
	minArgonMemory     = 19 * 1024
	maxArgonMemory     = 64 * 1024
	minArgonIterations = 2
	maxArgonIterations = 4
	minArgonLanes      = 1
	maxArgonLanes      = 4
)

type passwordDeriver func(password, salt []byte, iterations, memory uint32, parallelism uint8, keyLength uint32) []byte

type Credentials struct {
	username    string
	salt        []byte
	hash        []byte
	memory      uint32
	iterations  uint32
	parallelism uint8
	derive      passwordDeriver
}

// HashPassword returns an Argon2id PHC string using OWASP's 19 MiB, two
// iteration, one-lane profile. A 16-byte random salt and 32-byte key are used.
func HashPassword(password []byte, random io.Reader) (string, error) {
	if random == nil {
		return "", fmt.Errorf("password hashing: random source is required")
	}
	salt := make([]byte, passwordHashSaltLength)
	if _, err := io.ReadFull(random, salt); err != nil {
		return "", fmt.Errorf("password hashing: obtain salt: %w", err)
	}
	hash := argon2.IDKey(password, salt, passwordHashIterations, passwordHashMemory, passwordHashParallelism, passwordHashKeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		passwordHashMemory,
		passwordHashIterations,
		passwordHashParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func ParseCredentials(username, phc string) (*Credentials, error) {
	if username == "" || strings.TrimSpace(username) != username || len(username) > 128 {
		return nil, ErrInvalidPasswordHash
	}
	parsed, err := parsePasswordHash(phc)
	if err != nil {
		return nil, err
	}
	parsed.username = username
	parsed.derive = argon2.IDKey
	return parsed, nil
}

// Verify always performs the configured Argon2id derivation, including when
// the supplied username is unknown. Callers therefore expose one generic
// authentication result rather than a username oracle.
func (credentials *Credentials) Verify(username, password string) error {
	actual := credentials.derive(
		[]byte(password),
		credentials.salt,
		credentials.iterations,
		credentials.memory,
		credentials.parallelism,
		uint32(len(credentials.hash)),
	)
	passwordOK := subtle.ConstantTimeCompare(actual, credentials.hash)
	suppliedUsername := sha256.Sum256([]byte(username))
	configuredUsername := sha256.Sum256([]byte(credentials.username))
	usernameOK := subtle.ConstantTimeCompare(suppliedUsername[:], configuredUsername[:])
	if passwordOK&usernameOK != 1 {
		return ErrInvalidCredentials
	}
	return nil
}

// CredentialVersion binds sessions to the configured credential without
// persisting the PHC string itself. The fixed prefix provides domain separation.
func CredentialVersion(phc string) string {
	digest := sha256.Sum256(append([]byte("callegarin/admin-credential-version/v1\x00"), []byte(phc)...))
	return hex.EncodeToString(digest[:])
}

func parsePasswordHash(phc string) (*Credentials, error) {
	if len(phc) == 0 || len(phc) > maxPHCLength {
		return nil, ErrInvalidPasswordHash
	}
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return nil, ErrInvalidPasswordHash
	}

	var memory, iterations uint64
	var lanes uint64
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &lanes); err != nil || n != 3 || parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", memory, iterations, lanes) {
		return nil, ErrInvalidPasswordHash
	}
	if memory < minArgonMemory || memory > maxArgonMemory || iterations < minArgonIterations || iterations > maxArgonIterations || lanes < minArgonLanes || lanes > maxArgonLanes {
		return nil, ErrInvalidPasswordHash
	}
	if len(parts[4]) == 0 || len(parts[4]) > maxSaltEncodedLen || len(parts[5]) == 0 || len(parts[5]) > maxHashEncodedLen {
		return nil, ErrInvalidPasswordHash
	}
	salt, err := decodeRawBase64(parts[4])
	if err != nil || len(salt) < 16 || len(salt) > 32 {
		return nil, ErrInvalidPasswordHash
	}
	hash, err := decodeRawBase64(parts[5])
	if err != nil || len(hash) < 16 || len(hash) > 64 {
		return nil, ErrInvalidPasswordHash
	}

	return &Credentials{
		salt:        salt,
		hash:        hash,
		memory:      uint32(memory),
		iterations:  uint32(iterations),
		parallelism: uint8(lanes),
	}, nil
}

func decodeRawBase64(value string) ([]byte, error) {
	decoded, err := base64.RawStdEncoding.Strict().DecodeString(value)
	if err != nil || base64.RawStdEncoding.EncodeToString(decoded) != value {
		return nil, ErrInvalidPasswordHash
	}
	return decoded, nil
}
