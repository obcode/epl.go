package auth_test

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/obcode/tallox.go/internal/auth"
	"github.com/obcode/tallox.go/internal/testdata"
)

// TestMintedTokensParse closes the loop between the two halves of the format: what the server
// hands out has to be what the server accepts.
//
// The failure this prevents is a format that drifts on one side — a padding change, an extra
// character — and is only noticed by the first colleague who tries to use a token, at which
// point the token in their script is worthless and nobody can say why.
func TestMintedTokensParse(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}

	for range 50 {
		minted, err := auth.Mint()
		if err != nil {
			t.Fatalf("cannot mint: %v", err)
		}

		parsed, err := auth.ParseToken(minted.Plaintext)
		if err != nil {
			t.Fatalf("a freshly minted token does not parse: %v (%q)", err, minted.Plaintext)
		}
		if parsed.ID != minted.ID {
			t.Errorf("parsed id %q, minted %q", parsed.ID, minted.ID)
		}
		if len(minted.SecretHash) != 32 {
			t.Errorf("the stored hash is %d bytes, want 32 — the database CHECK would reject it",
				len(minted.SecretHash))
		}
		// The hash must be of the secret half, not of the whole token: the authenticator
		// hashes what it parsed out of the header.
		if string(auth.HashSecret(parsed.Secret)) != string(minted.SecretHash) {
			t.Error("the stored hash is not the hash of the secret half")
		}
		if seen[minted.ID] {
			t.Errorf("minted the same token id twice: %s", minted.ID)
		}
		seen[minted.ID] = true

		if !strings.HasPrefix(minted.Plaintext, "tallox_") {
			t.Errorf("minted token %q has no tallox_ prefix — gitleaks recognises tokens by "+
				"that shape, and the realistic leak is a colleague's script in a commit",
				minted.Plaintext)
		}
	}
}

// TestFixtureTokensParse keeps the shared fixtures usable by the real parser.
//
// A fixture the production parser would reject makes every test that uses it meaningless:
// the assertion passes against a token that could never arrive.
func TestFixtureTokensParse(t *testing.T) {
	t.Parallel()

	for _, p := range testdata.All() {
		parsed, err := auth.ParseToken(p.Token)
		if err != nil {
			t.Errorf("%s: the fixture token does not parse: %v", p.Name, err)
			continue
		}
		if parsed.ID != p.TokenID {
			t.Errorf("%s: parsed id %q, fixture says %q", p.Name, parsed.ID, p.TokenID)
		}
	}
}

// TestParseTokenRejectsWhatIsNotAToken covers the shapes that will actually arrive: a copied
// half, a truncated paste, somebody else's format, an id in the wrong alphabet.
func TestParseTokenRejectsWhatIsNotAToken(t *testing.T) {
	t.Parallel()

	valid, err := auth.Mint()
	if err != nil {
		t.Fatalf("cannot mint: %v", err)
	}
	id, secret, _ := strings.Cut(strings.TrimPrefix(valid.Plaintext, "tallox_"), "_")

	for _, tc := range []struct {
		name      string
		presented string
	}{
		{"empty", ""},
		{"no prefix", id + "_" + secret},
		{"wrong prefix", "epl_" + id + "_" + secret},
		{"no separator", "tallox_" + id + secret},
		{"only the id", "tallox_" + id},
		{"truncated secret", "tallox_" + id + "_" + secret[:len(secret)-1]},
		{"long secret", "tallox_" + id + "_" + secret + "x"},
		{"lower-case id", "tallox_" + strings.ToLower(id) + "_" + secret},
		// I, L, O and U are not in Crockford base32 precisely because they get misread; an id
		// containing one did not come from this server.
		{"ambiguous letters in the id", "tallox_IIIIIIIIIIIIIIII_" + secret},
		{"a JWT", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := auth.ParseToken(tc.presented); !errors.Is(err, auth.ErrMalformedToken) {
				t.Errorf("ParseToken(%q) returned %v, want ErrMalformedToken", tc.presented, err)
			}
		})
	}
}

// TestHashSecretIsStable: the hash is what is stored, so a change to it invalidates every
// token in the database at once. Pinning a known digest makes that a deliberate act.
func TestHashSecretIsStable(t *testing.T) {
	t.Parallel()

	// SHA-256 of "tallox". Verifiable without this code:  printf 'tallox' | sha256sum
	const want = "2df1a92994af843a201963226feb10657f6351a2dd1fc4eed894647f6e0dafdd"

	got := auth.HashSecret("tallox")
	if len(got) != 32 {
		t.Fatalf("hash is %d bytes, want 32", len(got))
	}
	if hex.EncodeToString(got) != want {
		t.Errorf("HashSecret changed. Every token in every database stops working the moment "+
			"this ships; if that is intended, it needs a migration and a note to the people "+
			"holding tokens.\ngot  %x\nwant %x", got, want)
	}
}
