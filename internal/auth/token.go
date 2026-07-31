package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"strings"
)

// The Personal Access Token format:
//
//	tallox_<16 characters, Crockford base32>_<43 characters, base64url>
//	       └─ the id: public, indexed, primary key   └─ the secret: 32 random bytes
//
// Two halves rather than one opaque blob, because authentication then costs one indexed
// lookup instead of a scan that hashes every candidate row — which is what makes a
// constant-time comparison affordable at all. The id is also what the audit log stores and
// what a colleague reads out over the phone; it says nothing that helps an attacker.
//
// The prefix exists so that a leaked token is recognisable as one. gitleaks matches exactly
// this shape (see .gitleaks.toml in both repositories), and the realistic leak path is not
// the server — it is an evaluation script committed with a token in it.
const (
	tokenPrefix = "tallox_"
	// tokenIDLength is 10 bytes of randomness in base32: ceil(80/5) = 16 characters.
	tokenIDLength = 16
	// secretLength is 32 bytes in unpadded base64url: ceil(256/6) = 43 characters.
	secretLength = 43
	// secretBytes is the entropy of the secret. 256 bits is why hashing it with SHA-256
	// rather than a KDF is sound: there is no dictionary of 32-byte random values.
	secretBytes = 32
	// idBytes is the entropy of the public id. It only has to be unique, not unguessable.
	idBytes = 10
)

// crockford is base32 without I, L, O and U — the letters that get misread as 1, 1, 0 and V.
// Token ids end up in support mails and are read aloud; the ambiguity is worth designing out.
var crockford = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// ParsedToken is a token split into the half that identifies it and the half that proves it.
type ParsedToken struct {
	// ID is the public half: safe to log, safe to index, safe to show in a token list.
	ID string
	// Secret is the half that must never be logged, stored or compared with ==.
	Secret string
}

// ParseToken splits a presented token, checking only its shape.
//
// Shape checking before the database is not about security — a malformed token cannot match
// anything — it is about not turning every typo into a query. The error is deliberately the
// same for every kind of malformation: which part is wrong is not information a caller who
// does not hold a valid token needs.
func ParseToken(presented string) (ParsedToken, error) {
	rest, found := strings.CutPrefix(presented, tokenPrefix)
	if !found {
		return ParsedToken{}, fmt.Errorf("%w: missing the %s prefix", ErrMalformedToken, tokenPrefix)
	}

	id, secret, found := strings.Cut(rest, "_")
	if !found {
		return ParsedToken{}, fmt.Errorf("%w: no separator between id and secret", ErrMalformedToken)
	}
	if len(id) != tokenIDLength || len(secret) != secretLength {
		return ParsedToken{}, fmt.Errorf("%w: expected %d and %d characters, got %d and %d",
			ErrMalformedToken, tokenIDLength, secretLength, len(id), len(secret))
	}
	if strings.TrimLeft(id, crockfordAlphabet) != "" {
		return ParsedToken{}, fmt.Errorf("%w: the id is not Crockford base32", ErrMalformedToken)
	}

	return ParsedToken{ID: id, Secret: secret}, nil
}

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// HashSecret is how a secret becomes the value stored in the database.
//
// SHA-256, not argon2id, and that is a decision rather than an oversight. A password KDF buys
// resistance against offline brute force of *guessable* secrets; this secret is 32 bytes from
// crypto/rand, so there is nothing to guess. What the KDF would cost is ~50 ms of CPU and
// tens of megabytes of RAM on every single API call — a colleague's 500-query evaluation
// script would spend 25 seconds of server CPU on nothing, and anybody inside the VPN could
// exhaust memory by sending garbage tokens quickly.
//
// The precondition, which has to stay true: the server always generates the secret. There is
// no bring-your-own-token path. If one is ever added, this stops being sound and both the
// hash and this comment have to change together.
func HashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// MintedToken is a freshly generated token: the string to show the owner exactly once, and
// the two values to store.
type MintedToken struct {
	// Plaintext is the whole token. It exists in memory once, is shown once, and is never
	// written down by this system.
	Plaintext string
	// ID is the public half, stored as the primary key.
	ID string
	// SecretHash is what the database keeps.
	SecretHash []byte
}

// Mint generates a new Personal Access Token.
//
// Both halves come from crypto/rand. The id has to be unique rather than unguessable, but
// there is no reason to make it anything else, and 80 bits makes a collision on the primary
// key a non-event rather than a retry loop somebody has to write.
func Mint() (MintedToken, error) {
	idRaw := make([]byte, idBytes)
	if _, err := rand.Read(idRaw); err != nil {
		return MintedToken{}, fmt.Errorf("cannot generate a token id: %w", err)
	}
	secretRaw := make([]byte, secretBytes)
	if _, err := rand.Read(secretRaw); err != nil {
		return MintedToken{}, fmt.Errorf("cannot generate a token secret: %w", err)
	}

	id := crockford.EncodeToString(idRaw)
	secret := base64.RawURLEncoding.EncodeToString(secretRaw)

	return MintedToken{
		Plaintext:  tokenPrefix + id + "_" + secret,
		ID:         id,
		SecretHash: HashSecret(secret),
	}, nil
}
