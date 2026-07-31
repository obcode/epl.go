package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/obcode/tallox.go/internal/auth"
	"github.com/obcode/tallox.go/internal/policy"
	"github.com/obcode/tallox.go/internal/store"
	"github.com/obcode/tallox.go/internal/store/storetest"
	"github.com/obcode/tallox.go/internal/testdata"
)

// secretOf pulls the secret half out of a fixture token, so that what is stored is what the
// real authenticator will hash and compare.
func secretOf(t *testing.T, p testdata.Persona) string {
	t.Helper()

	parsed, err := auth.ParseToken(p.Token)
	if err != nil {
		t.Fatalf("fixture token of %s does not parse: %v", p.Name, err)
	}
	return parsed.Secret
}

// TestDirectoryAnswersTheBrowserDoor covers the adapter between the generated queries and the
// seam internal/auth defines — against real PostgreSQL, because the interesting part of the
// answer (citext matching, the aggregated roles) is produced by the database and not by Go.
func TestDirectoryAnswersTheBrowserDoor(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	storetest.SeedPerson(t, s, testdata.Vier,
		string(policy.RoleDozent), string(policy.RoleStudiengangsleitung))

	d := store.NewDirectory(s.Pool)

	got, err := d.PersonByMail(t.Context(), strings.ToUpper(testdata.Vier.Mail))
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if got == nil {
		t.Fatal("a seeded person was not found by a differently-cased address")
	}
	if got.ID != testdata.Vier.ID() || got.Mail != testdata.Vier.Mail {
		t.Errorf("resolved to %v/%s", got.ID, got.Mail)
	}
	if !got.Active {
		t.Error("a seeded person is not active")
	}
	if len(got.Roles) != 2 {
		t.Errorf("roles are %v, want both grants", got.Roles)
	}
}

// TestUnknownIdentitiesAreNotAnError pins the (nil, nil) convention on the path where it
// carries the most weight.
//
// Authentication has to distinguish "this installation has never heard of you" — a 401 that
// says so, and an import that has not run yet — from "the database is unreachable", which is
// a 503 and nobody's credential is broken. If a missing row arrived as an error, a restart of
// PostgreSQL and a new colleague's first login would produce the same message.
func TestUnknownIdentitiesAreNotAnError(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	d := store.NewDirectory(s.Pool)

	person, err := d.PersonByMail(t.Context(), "niemand@example.org")
	if err != nil {
		t.Errorf("an unknown mail address produced an error: %v", err)
	}
	if person != nil {
		t.Errorf("an unknown mail address produced %+v", person)
	}

	token, err := d.TokenByID(t.Context(), "ZZZZZZZZZZZZZZZZ")
	if err != nil {
		t.Errorf("an unknown token id produced an error: %v", err)
	}
	if token != nil {
		t.Errorf("an unknown token id produced %+v", token)
	}
}

// TestDirectoryAnswersTheTokenDoor covers the second seam, including the two conversions that
// are easy to get wrong in opposite directions: the stored hash has to come back byte for
// byte, and a NULL revoked_at has to become the zero time rather than a driver wrapper that
// every caller would have to know about.
func TestDirectoryAnswersTheTokenDoor(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	storetest.SeedPerson(t, s, testdata.Eins, string(policy.RoleDozent))
	storetest.SeedToken(t, s, testdata.Eins,
		auth.HashSecret(secretOf(t, testdata.Eins)),
		storetest.TokenOptions{Scopes: []string{"wishes:read"}})

	d := store.NewDirectory(s.Pool)

	got, err := d.TokenByID(t.Context(), testdata.Eins.TokenID)
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if got == nil {
		t.Fatal("a seeded token was not found")
	}

	if string(got.SecretHash) != string(auth.HashSecret(secretOf(t, testdata.Eins))) {
		t.Error("the hash came back different from what was stored — every token would be " +
			"rejected, and the message would say 'invalid'")
	}
	if !got.RevokedAt.IsZero() {
		t.Errorf("an unrevoked token reports RevokedAt = %v; the authenticator refuses "+
			"anything non-zero, so every token would look withdrawn", got.RevokedAt)
	}
	if !got.ExpiresAt.After(time.Now()) {
		t.Error("the seeded expiry is not in the future")
	}
	if got.Owner.ID != testdata.Eins.ID() || !got.Owner.Active {
		t.Errorf("owner is %+v", got.Owner)
	}
	if len(got.Owner.Roles) != 1 {
		t.Errorf("owner roles are %v", got.Owner.Roles)
	}
}

// TestRevokedTokensCarryTheirTimestamp: the other half of the conversion above. A revoked
// token has to arrive with a non-zero time, because that is the only thing the authenticator
// tests.
func TestRevokedTokensCarryTheirTimestamp(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	storetest.SeedPerson(t, s, testdata.Eins)
	storetest.SeedToken(t, s, testdata.Eins,
		auth.HashSecret(secretOf(t, testdata.Eins)),
		storetest.TokenOptions{Revoked: true})

	got, err := store.NewDirectory(s.Pool).TokenByID(t.Context(), testdata.Eins.TokenID)
	if err != nil || got == nil {
		t.Fatalf("lookup failed: %v (%v)", err, got)
	}
	if got.RevokedAt.IsZero() {
		t.Error("a revoked token reports no revocation time — it would keep working")
	}
}

// TestMarkTokenUsedGoesThroughTheAdapter: the call is made from a goroutine off the request
// path, where an error only reaches a log line. Exercising it here is what keeps a broken
// query from being invisible.
func TestMarkTokenUsedGoesThroughTheAdapter(t *testing.T) {
	t.Parallel()

	s := storetest.New(t)
	storetest.SeedPerson(t, s, testdata.Eins)
	storetest.SeedToken(t, s, testdata.Eins,
		auth.HashSecret(secretOf(t, testdata.Eins)), storetest.TokenOptions{})

	if err := store.NewDirectory(s.Pool).MarkTokenUsed(t.Context(), testdata.Eins.TokenID); err != nil {
		t.Fatalf("cannot record use: %v", err)
	}

	rows, err := s.Queries().ListTokensOfPerson(t.Context(), testdata.Eins.ID())
	if err != nil || len(rows) != 1 {
		t.Fatalf("cannot list tokens: %v (%d)", err, len(rows))
	}
	if !rows[0].LastUsedAt.Valid {
		t.Error("last_used_at was not written")
	}
}
