package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// SHA256Hex returns the lowercase hex SHA-256 digest of b.
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// CanonicalJSON marshals v deterministically: encoding/json emits struct
// fields in declaration order and sorts map keys, which gives a stable byte
// representation for identical logical content.
func CanonicalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Digest returns the canonical SHA-256 digest of v. It is the single source
// of truth for topology digests, rule digests, and idempotency request
// summaries: identical logical content always produces an identical digest.
func Digest(v any) (string, error) {
	b, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	return SHA256Hex(b), nil
}

// MustDigest is Digest that panics on error. It is intended for values known
// to be marshalable (plain data records) and for test fixtures.
func MustDigest(v any) string {
	d, err := Digest(v)
	if err != nil {
		panic(err)
	}
	return d
}
