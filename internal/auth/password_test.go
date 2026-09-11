package auth

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestHashPasswordAndVerify(t *testing.T) {
	phc, err := HashPassword([]byte("correct horse battery staple"), bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)))
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(phc, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected PHC parameters: %q", phc)
	}

	credentials, err := ParseCredentials("admin", phc)
	if err != nil {
		t.Fatalf("ParseCredentials: %v", err)
	}
	if err := credentials.Verify("admin", "correct horse battery staple"); err != nil {
		t.Fatalf("Verify valid credentials: %v", err)
	}
	if err := credentials.Verify("admin", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Verify wrong password = %v, want ErrInvalidCredentials", err)
	}
	if err := credentials.Verify("someone-else", "correct horse battery staple"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Verify wrong username = %v, want ErrInvalidCredentials", err)
	}
}

func TestVerifyAlwaysDerivesPasswordForUnknownUsername(t *testing.T) {
	phc, err := HashPassword([]byte("password"), bytes.NewReader(bytes.Repeat([]byte{0x24}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := ParseCredentials("admin", phc)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	original := credentials.derive
	credentials.derive = func(password, salt []byte, iterations uint32, memory uint32, parallelism uint8, keyLength uint32) []byte {
		calls++
		return original(password, salt, iterations, memory, parallelism, keyLength)
	}
	if err := credentials.Verify("unknown", "password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Verify = %v, want ErrInvalidCredentials", err)
	}
	if calls != 1 {
		t.Fatalf("password derivations = %d, want 1", calls)
	}
}

func TestParseCredentialsRejectsMalformedOrUnsafePHCBeforeDerivation(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 16))
	hash := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	tests := map[string]string{
		"wrong algorithm":     "$argon2i$v=19$m=19456,t=2,p=1$" + salt + "$" + hash,
		"wrong version":       "$argon2id$v=16$m=19456,t=2,p=1$" + salt + "$" + hash,
		"parameter order":     "$argon2id$v=19$t=2,m=19456,p=1$" + salt + "$" + hash,
		"duplicate parameter": "$argon2id$v=19$m=19456,t=2,p=1,p=1$" + salt + "$" + hash,
		"excessive memory":    "$argon2id$v=19$m=1048577,t=2,p=1$" + salt + "$" + hash,
		"weak memory":         "$argon2id$v=19$m=8192,t=2,p=1$" + salt + "$" + hash,
		"excessive time":      "$argon2id$v=19$m=19456,t=99,p=1$" + salt + "$" + hash,
		"weak time":           "$argon2id$v=19$m=19456,t=1,p=1$" + salt + "$" + hash,
		"excessive lanes":     "$argon2id$v=19$m=19456,t=2,p=99$" + salt + "$" + hash,
		"short salt":          "$argon2id$v=19$m=19456,t=2,p=1$" + base64.RawStdEncoding.EncodeToString([]byte("short")) + "$" + hash,
		"oversized hash":      "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$" + strings.Repeat("A", 500),
		"base64 padding":      "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "=$" + hash,
		"trailing field":      "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$" + hash + "$extra",
		"oversized input":     strings.Repeat("x", 513),
	}
	for name, phc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCredentials("admin", phc); !errors.Is(err, ErrInvalidPasswordHash) {
				t.Fatalf("ParseCredentials error = %v, want ErrInvalidPasswordHash", err)
			}
		})
	}
}

func TestCredentialVersionIsStableDomainSeparatedDigest(t *testing.T) {
	one := CredentialVersion("$argon2id$one")
	two := CredentialVersion("$argon2id$two")
	if len(one) != 64 || one == two {
		t.Fatalf("unexpected credential versions: %q %q", one, two)
	}
	if one != CredentialVersion("$argon2id$one") {
		t.Fatal("credential version is not stable")
	}
}

func TestPasswordLengthBoundaryIsSharedByHashing(t *testing.T) {
	maximum := bytes.Repeat([]byte{'x'}, MaxPasswordBytes)
	if _, err := HashPassword(maximum, bytes.NewReader(bytes.Repeat([]byte{0x51}, 16))); err != nil {
		t.Fatalf("HashPassword(maximum): %v", err)
	}
	tooLong := append(append([]byte(nil), maximum...), 'x')
	if _, err := HashPassword(tooLong, bytes.NewReader(bytes.Repeat([]byte{0x52}, 16))); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("HashPassword(maximum+1) error = %v, want ErrInvalidPassword", err)
	}
	if err := ValidatePassword(nil); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("ValidatePassword(empty) error = %v", err)
	}
}
