package policy_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/obcode/tallox.go/internal/golden"
	"github.com/obcode/tallox.go/internal/policy"
	"github.com/obcode/tallox.go/internal/principal"
	"github.com/obcode/tallox.go/internal/testdata"
)

// TestVisibilityMatrix renders the whole wish-visibility rule as a table and compares it
// against a committed file.
//
// Two jobs, and the second one is why this exists in week one, before the wish workflow does:
//
//  1. Every change to who sees what becomes a reviewable diff instead of a sentence in a
//     commit message. A rule that is only readable by executing it in your head is a rule that
//     gets widened by accident.
//  2. The file is the slide for the faculty retreat. "Wer sieht was, wann" is the question
//     this project will be asked in front of a room, and the answer should be a printed table
//     generated from the code that enforces it — not a drawing that agrees with it today.
//
// Re-record with: go test ./internal/policy/ -update-golden — and then read the diff.
func TestVisibilityMatrix(t *testing.T) {
	t.Parallel()

	golden.Assert(t, "visibility_matrix", renderMatrix())
}

// caller is one row group of the matrix: who is asking, through which door.
type caller struct {
	label string
	door  string
	actor principal.Actor
}

func callers() []caller {
	out := []caller{{
		label: "(not signed in)",
		door:  "—",
		actor: principal.Anonymous,
	}}

	for _, role := range policy.AllRoles() {
		for _, door := range []struct {
			label string
			kind  principal.Kind
		}{
			{"interactive", principal.KindInteractive},
			{"token", principal.KindToken},
		} {
			out = append(out, caller{
				label: string(role),
				door:  door.label,
				actor: testdata.Eins.Actor(door.kind, string(role)),
			})
		}
	}
	return out
}

func renderMatrix() string {
	var b strings.Builder

	b.WriteString(matrixPreamble)

	header := row("Role", "Door", "Phase", "Wishes", "own", "other", "Filter")
	b.WriteString(header)
	b.WriteString(rule(header))

	for _, c := range callers() {
		for _, state := range []struct {
			label string
			state policy.SemesterState
		}{
			{"unpublished", unpublished},
			{"published", published},
		} {
			for _, phase := range policy.AllPhases() {
				s := state.state
				s.Phase = phase

				own := policy.Wish{OwnerID: c.actor.ID}
				other := policy.Wish{OwnerID: testdata.Zwei.ID()}

				b.WriteString(row(
					c.label,
					c.door,
					string(phase),
					state.label,
					answer(policy.CanSeeWish(c.actor, s, own)),
					answer(policy.CanSeeWish(c.actor, s, other)),
					filterLabel(policy.WishVisibility(c.actor, s)),
				))
			}
		}
		// One blank line per caller: the table is read by eye, on a slide, by somebody looking
		// for their own row.
		b.WriteString("\n")
	}

	return b.String()
}

const matrixPreamble = `Wish visibility — who sees which wish, and when
===============================================

Generated from internal/policy (TestVisibilityMatrix). Do not edit by hand:

    go test ./internal/policy/ -update-golden

The rule
--------

A wish is visible if and only if

  · it belongs to the person asking, or
  · the wishes of that semester have been published
    (semester.wishes_published_at IS NOT NULL), or
  · that person plans (subject group lead or programme lead) or belongs to the dean's office
    — and then only in an interactive session, never through a Personal Access Token.

The purpose is to end the first-come-first-served race: a new colleague should be able to
register interest without it looking like an attack on somebody who has taught the subject
for years.

The columns
-----------

  own / other   Does this person see their own wish / somebody else's?
  Filter        The same rule as a query restriction. **Counts run through exactly this
                filter** — otherwise "three colleagues have already registered interest"
                gives the confidential answer away in full, without naming anybody.

Notes
-----

  · The phase appears in every row and never changes the answer. Publication is a timestamp
    of its own, not a consequence of the phase: the wish phase can end without publishing,
    and publication can happen while the assignment is already running.
  · Combinations of roles are not listed. Somebody holding several sees the union — checked
    over the complete cartesian product in TestGuardAndFilterAgree.
  · ADMIN is deliberately not a wish reader. Running the system is a different job from
    planning with it; an administrator who genuinely needs to look is granted DEANS_OFFICE,
    visibly.
  · Through a Personal Access Token, even a planner sees only their own wishes until
    publication. A long-lived token in a script makes silent bulk export possible and
    decouples "who saw this" from any login event.

`

// widths are the column widths, in runes. Fixed rather than computed, so that adding a role
// with a long name produces a visible one-line change here instead of reflowing the entire
// file and drowning the actual diff.
var widths = []int{20, 13, 17, 13, 6, 7, 10}

func row(cells ...string) string {
	var b strings.Builder
	for i, c := range cells {
		if i == len(cells)-1 {
			b.WriteString(c)
			break
		}
		b.WriteString(pad(c, widths[i]))
	}
	b.WriteString("\n")
	return b.String()
}

func rule(header string) string {
	return strings.Repeat("-", utf8.RuneCountInString(strings.TrimRight(header, "\n"))) + "\n"
}

// pad left-aligns to n columns, counting runes.
//
// Not %-20s: that pads to a byte width, and half the words in this table (Tür, Wünsche,
// unveröffentlicht) contain multi-byte runes. The result would be a table that looks aligned
// to fmt and ragged to a reader — on the one artefact whose whole purpose is to be read.
func pad(s string, n int) string {
	if missing := n - utf8.RuneCountInString(s); missing > 0 {
		return s + strings.Repeat(" ", missing)
	}
	return s + " "
}

func answer(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}

func filterLabel(f policy.WishFilter) string {
	switch f.Scope {
	case policy.WishScopeAll:
		return "all"
	case policy.WishScopeOwn:
		return "own only"
	case policy.WishScopeNone:
		return "no access"
	default:
		return fmt.Sprintf("?? %s", f.Scope)
	}
}
