// Package testdata holds the invented people every test in this repository shares.
//
// # Why a package and not a literal in each test
//
// Two reasons, and the second is the important one.
//
// The mundane reason is that visibility rules are always about a relationship: an owner, a
// colleague who must not see, someone whose role overrides both. Written inline, those roles
// get re-invented per test with different names, and a reader has to reconstruct which
// address was supposed to be the owner before they can judge whether the assertion is right.
//
// The reason that actually matters: this repository is public. The tempting way to write a
// fixture is to reach for a name that comes to mind, and the names that come to mind are the
// colleagues whose teaching assignments this system will manage — people who never agreed to
// appear in a public repository next to an overtime column. Providing a ready set of invented
// people removes the moment where someone has to invent one under time pressure.
//
// Everything here is fictional. The addresses are under example.org, which RFC 2606 reserves
// for exactly this, so no test can accidentally send mail to a real person.
package testdata

// Persona is one invented person, described by the part they play in a test rather than by an
// organisational role.
//
// Roles (DOZENT, PLANER, …) deliberately do not appear here. They belong to internal/policy,
// which is where the rules that use them live; duplicating them into fixtures would create a
// second list to keep in sync, and a stale role in a fixture makes a policy test pass while
// the policy is wrong.
type Persona struct {
	// Mail is the identity the auth proxy would put in X-Remote-User.
	Mail string
	// Name is what a GUI would render.
	Name string
	// Token is a Personal Access Token belonging to this persona, in the production format.
	//
	// The 16 A's are what the gitleaks allowlist recognises as "obviously not a real secret"
	// — see .gitleaks.toml. Keep that prefix on any token added here, or committing the
	// fixture will be blocked by the pre-commit hook.
	Token string
	// Part is the role this persona plays in a test scenario, in prose. Read it before using
	// the persona for something else: swapping two personas around silently inverts what a
	// visibility test proves, and the test still passes.
	Part string
}

// The cast. Deliberately small: a fixture set large enough that nobody remembers who is who
// is a fixture set where tests get written against the wrong person.
var (
	// Eins owns things. The wish under test is hers, the competence entry is hers, and she is
	// the one for whom the answer is always "yes, you may see it".
	Eins = Persona{
		Mail:  "prof.eins@example.org",
		Name:  "Prof. Eins",
		Token: "tallox_AAAAAAAAAAAAAAAA_eins000000000000000000000000000000000000000",
		Part:  "owns the record under test",
	}

	// Zwei is the colleague. Same role as Eins, no relationship to the record — the person
	// the confidentiality rule exists to withhold information from. Most visibility tests are
	// really the question "what does Zwei see", and the interesting answer is "nothing, until
	// publication".
	Zwei = Persona{
		Mail:  "prof.zwei@example.org",
		Name:  "Prof. Zwei",
		Token: "tallox_AAAAAAAAAAAAAAAA_zwei000000000000000000000000000000000000000",
		Part:  "an unrelated colleague at the same level",
	}

	// Drei leads a Fachgruppe: allowed to assign within her group, not beyond it. The persona
	// for "the permission exists but is scoped", which is the case a boolean role check gets
	// wrong.
	Drei = Persona{
		Mail:  "prof.drei@example.org",
		Name:  "Prof. Drei",
		Token: "tallox_AAAAAAAAAAAAAAAA_drei000000000000000000000000000000000000000",
		Part:  "leads one Fachgruppe — permitted inside it, not outside",
	}

	// Vier plans. Sees wishes before publication because the process requires it, which makes
	// her the persona that proves the rule has an exception rather than a hole.
	Vier = Persona{
		Mail:  "prof.vier@example.org",
		Name:  "Prof. Vier",
		Token: "tallox_AAAAAAAAAAAAAAAA_vier000000000000000000000000000000000000000",
		Part:  "plans — sees unpublished wishes by design",
	}

	// Fuenf is the dean's office: reads everything, including across programmes, and changes
	// almost nothing. Separate from Vier because "may read" and "may write" are different
	// axes and a test that conflates them will not notice when they get conflated in code.
	Fuenf = Persona{
		Mail:  "dekanat@example.org",
		Name:  "Dekanat",
		Token: "tallox_AAAAAAAAAAAAAAAA_fuenf00000000000000000000000000000000000000",
		Part:  "dean's office — reads across programmes, writes little",
	}

	// Anon is nobody: no proxy header, no token. The persona for the questions that are easy
	// to forget to ask, such as which fields answer before a session exists.
	Anon = Persona{
		Mail:  "",
		Name:  "",
		Token: "",
		Part:  "unauthenticated",
	}
)

// All returns the authenticated personas, for tests that iterate over everyone.
//
// Anon is not in the list: a loop over "all people" that silently includes an unauthenticated
// caller produces assertions that are wrong for one entry and right for the rest, which is
// the kind of test that gets a special case bolted on instead of being fixed.
func All() []Persona {
	return []Persona{Eins, Zwei, Drei, Vier, Fuenf}
}

// Others returns every authenticated persona except the given one — the natural argument for
// "and nobody else may see it".
func Others(p Persona) []Persona {
	out := make([]Persona, 0, len(All()))
	for _, candidate := range All() {
		if candidate.Mail != p.Mail {
			out = append(out, candidate)
		}
	}
	return out
}

// Mails returns the addresses of the given personas. Handy as the forbidden list for
// graphqltest.AssertNoLeak: no user-facing error may name someone else.
func Mails(personas []Persona) []string {
	out := make([]string, 0, len(personas))
	for _, p := range personas {
		out = append(out, p.Mail)
	}
	return out
}
