package testdata_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/obcode/tallox.go/internal/testdata"
)

// tokenFormat is the production Personal Access Token shape, copied from .gitleaks.toml.
// Fixtures that do not match it would let a token-parsing test pass against data the parser
// will never see in production.
var tokenFormat = regexp.MustCompile(`^tallox_[0-9A-HJKMNP-TV-Z]{16}_[A-Za-z0-9_-]{43}$`)

// TestPersonasAreFictionalAndDistinct guards the rule that this repository is public.
//
// The failure it prevents is not a crash. It is a fixture that quietly names a real
// colleague, in a public repository, next to a Deputat column — a thing that is trivially
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

	for _, p := range testdata.All() {
		if !tokenFormat.MatchString(p.Token) {
			t.Errorf("%s: token %q does not match the production format — a fixture the real "+
				"parser would reject makes a passing test meaningless", p.Name, p.Token)
		}
		// The all-A prefix is what .gitleaks.toml allowlists as "obviously not a secret".
		// Without it the pre-commit hook blocks the commit that adds the persona.
		if !strings.HasPrefix(p.Token, "tallox_AAAAAAAAAAAAAAAA_") {
			t.Errorf("%s: token does not use the gitleaks-allowlisted AAAA prefix, so "+
				"committing it will be blocked", p.Name)
		}
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
