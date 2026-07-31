package testdata_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/obcode/tallox.go/internal/principal"
	"github.com/obcode/tallox.go/internal/testdata"
)

// tokenFormat is the production Personal Access Token shape, copied from .gitleaks.toml.
// Fixtures that do not match it would let a token-parsing test pass against data the parser
// will never see in production.
var tokenFormat = regexp.MustCompile(`^tallox_[0-9A-HJKMNP-TV-Z]{16}_[A-Za-z0-9_-]{43}$`)

// TestPersonasAreFictionalAndDistinct guards the rule that this repository is public.
//
// The failure it prevents is not a crash. It is a fixture that quietly names a real
// colleague, in a public repository, next to a teaching-load column — a thing that is trivially
// avoided at the moment of writing and awkward to undo once it is in the git history of a
// public remote.
func TestPersonasAreFictionalAndDistinct(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}

	for _, p := range testdata.All() {
		if !strings.HasSuffix(p.Mail, "@example.org") {
			t.Errorf("%s: mail %q is not under example.org — RFC 2606 reserves that domain "+
				"so a fixture can never reach a real mailbox", p.Name, p.Mail)
		}
		if p.Part == "" {
			t.Errorf("%s has no Part — a persona without a documented role in a scenario "+
				"gets used for the wrong one", p.Name)
		}
		if previous, dup := seen[p.Mail]; dup {
			t.Errorf("%s and %s share the mail address %q — two personas that are the same "+
				"person make every visibility assertion between them vacuous",
				previous, p.Name, p.Mail)
		}
		seen[p.Mail] = p.Name
	}
}

// TestPersonaTokensMatchTheProductionFormat keeps the fixtures parseable by the real token
// parser, and keeps them recognisable to gitleaks.
func TestPersonaTokensMatchTheProductionFormat(t *testing.T) {
	t.Parallel()

	ids := map[string]string{}

	for _, p := range testdata.All() {
		if !tokenFormat.MatchString(p.Token) {
			t.Errorf("%s: token %q does not match the production format — a fixture the real "+
				"parser would reject makes a passing test meaningless", p.Name, p.Token)
		}
		// The "example" secret prefix is what .gitleaks.toml allowlists as "obviously not a
		// secret". Without it the pre-commit hook blocks the commit that adds the persona.
		if !strings.HasPrefix(p.Token, "tallox_"+p.TokenID+"_example") {
			t.Errorf("%s: token %q is not tallox_<TokenID>_example… — either the id and the "+
				"token have drifted apart, or the gitleaks allowlist will not recognise it "+
				"and committing the fixture will be blocked", p.Name, p.Token)
		}
		// Distinct ids, because the token id is the primary key of the token table: a cast
		// that shares one cannot be seeded into a single schema, and the natural repair is to
		// seed fewer people — which removes the colleague the confidentiality rule is about.
		if previous, dup := ids[p.TokenID]; dup {
			t.Errorf("%s and %s share the token id %q — they cannot both exist in one "+
				"database", previous, p.Name, p.TokenID)
		}
		ids[p.TokenID] = p.Name
	}
}

// TestPersonaIDsAreStableAndDistinct covers what a derived id has to be good for: the same
// persona is the same person in a store test, in an API test and in a golden file, and no two
// personas are accidentally the same row.
//
// Anon is the case worth stating out loud. An unauthenticated caller has uuid.Nil, which is
// also what an unset owner column holds — the pair principal.Actor.Owns exists to refuse.
func TestPersonaIDsAreStableAndDistinct(t *testing.T) {
	t.Parallel()

	if testdata.Anon.ID() != uuid.Nil {
		t.Errorf("Anon has id %v, want the nil UUID", testdata.Anon.ID())
	}

	seen := map[uuid.UUID]string{}
	for _, p := range testdata.All() {
		id := p.ID()
		if id == uuid.Nil {
			t.Errorf("%s has the nil UUID, which no authenticated persona may have", p.Name)
		}
		if id != p.ID() {
			t.Errorf("%s: ID() is not stable across calls", p.Name)
		}
		if previous, dup := seen[id]; dup {
			t.Errorf("%s and %s derive the same id %v", previous, p.Name, id)
		}
		seen[id] = p.Name
	}
}

// TestActorCarriesTheDoorItCameThrough: the token id belongs on a token actor and nowhere
// else. It is what the audit log resolves after a token has been revoked, and a browser
// session that carried one would attribute a person's clicks to a script.
func TestActorCarriesTheDoorItCameThrough(t *testing.T) {
	t.Parallel()

	browser := testdata.Eins.Actor(principal.KindInteractive, "LECTURER")
	if browser.TokenID != "" {
		t.Errorf("an interactive actor carries token id %q", browser.TokenID)
	}
	if !browser.Authenticated() || browser.ID != testdata.Eins.ID() {
		t.Errorf("actor id is %v, want %v", browser.ID, testdata.Eins.ID())
	}

	token := testdata.Eins.Actor(principal.KindToken, "LECTURER")
	if token.TokenID != testdata.Eins.TokenID {
		t.Errorf("token actor carries token id %q, want %q", token.TokenID, testdata.Eins.TokenID)
	}
	if token.ID != browser.ID {
		t.Error("the same persona has different ids on the two doors — a token would not be " +
			"the same principal as its owner, which is the whole invariant")
	}
}

// TestAnonIsNotInAll pins the deliberate omission.
//
// A loop over "everyone" that includes an unauthenticated caller yields assertions that are
// wrong for exactly one entry — and the usual repair is a special case inside the loop rather
// than a fix, after which the loop no longer tests what its name says.
func TestAnonIsNotInAll(t *testing.T) {
	t.Parallel()

	for _, p := range testdata.All() {
		if p.Mail == testdata.Anon.Mail {
			t.Fatal("Anon appears in All() — see the doc comment on All")
		}
	}
}

// TestOthersExcludesExactlyOne covers the helper most visibility tests will lean on for their
// "and nobody else" half.
func TestOthersExcludesExactlyOne(t *testing.T) {
	t.Parallel()

	others := testdata.Others(testdata.Eins)

	if len(others) != len(testdata.All())-1 {
		t.Fatalf("Others returned %d personas, want %d", len(others), len(testdata.All())-1)
	}
	for _, p := range others {
		if p.Mail == testdata.Eins.Mail {
			t.Errorf("Others(Eins) still contains Eins")
		}
	}
}

// TestMailsFeedsTheLeakDetector: Mails exists so a test can say "this message may not name
// anyone else" in one line. If it ever returned empty, AssertNoLeak would check nothing and
// pass, which is the worst way for a guard to fail.
func TestMailsFeedsTheLeakDetector(t *testing.T) {
	t.Parallel()

	mails := testdata.Mails(testdata.Others(testdata.Eins))

	if len(mails) == 0 {
		t.Fatal("Mails returned nothing — a forbidden list that is empty makes AssertNoLeak " +
			"pass unconditionally")
	}
	for _, m := range mails {
		if m == "" {
			t.Error("Mails returned an empty address, which AssertNoLeak has to ignore")
		}
	}
}
