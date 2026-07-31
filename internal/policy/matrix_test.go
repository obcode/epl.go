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
// Which is why it is rendered in German, unlike everything else in this repository: it is read
// by the colleagues the rule protects, not only by whoever maintains it.
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
		label: "(nicht angemeldet)",
		door:  "—",
		actor: principal.Anonymous,
	}}

	for _, role := range policy.AllRoles() {
		for _, door := range []struct {
			label string
			kind  principal.Kind
		}{
			{"interaktiv", principal.KindInteractive},
			{"Token", principal.KindToken},
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

	header := row("Rolle", "Tür", "Phase", "Wünsche", "eigener", "fremder", "Filter")
	b.WriteString(header)
	b.WriteString(rule(header))

	for _, c := range callers() {
		for _, state := range []struct {
			label string
			state policy.SemesterState
		}{
			{"unveröffentlicht", unpublished},
			{"veröffentlicht", published},
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

const matrixPreamble = `Wunsch-Sichtbarkeit — wer sieht welchen Wunsch, wann
===================================================

Erzeugt aus internal/policy (TestVisibilityMatrix). Nicht von Hand ändern:

    go test ./internal/policy/ -update-golden

Die Regel
---------

Ein Wunsch ist sichtbar genau dann, wenn

  · er der anfragenden Person selbst gehört, oder
  · die Wünsche des Semesters veröffentlicht sind
    (semester.wishes_published_at IS NOT NULL), oder
  · die Person plant (Fachgruppen- oder Studiengangsleitung) bzw. zum Dekanat gehört —
    und dann nur in einer interaktiven Sitzung, nicht über ein Personal Access Token.

Zweck: kein Windhundverfahren. Neue Kolleg:innen sollen sich eintragen können, ohne dass es
wie ein Angriff auf eine alteingesessene Person wirkt.

Die Spalten
-----------

  eigener / fremder   Sieht diese Person ihren eigenen bzw. einen fremden Wunsch?
  Filter              Dieselbe Regel als Query-Einschränkung. **Zählungen laufen durch
                      genau diesen Filter** — sonst verrät „3 Kolleg:innen haben bereits
                      Interesse" die Information vollständig, ohne einen Namen zu nennen.

Anmerkungen
-----------

  · Die Phase steht in jeder Zeile und ändert die Antwort nie. Veröffentlicht wird über einen
    eigenen Zeitstempel, nicht über die Phase: die Wunschphase kann enden, ohne dass
    veröffentlicht wird, und veröffentlicht werden, während die Zuteilung läuft.
  · Mehrfachrollen sind nicht aufgeführt. Wer mehrere Rollen hält, sieht die Vereinigung —
    geprüft über das vollständige Kreuzprodukt in TestGuardAndFilterAgree.
  · ADMIN ist bewusst kein Wunsch-Leser. Das System zu betreiben ist eine andere Aufgabe als
    damit zu planen; wer wirklich hineinsehen muss, bekommt sichtbar die Rolle DEKANAT.
  · Über ein Personal Access Token sieht auch eine planende Person nur die eigenen Wünsche,
    solange nicht veröffentlicht ist. Ein langlebiges Token in einem Skript ermöglicht
    stillen Massenexport und entkoppelt „wer hat das gesehen" von einem Login-Ereignis.

`

// widths are the column widths, in runes. Fixed rather than computed, so that adding a role
// with a long name produces a visible one-line change here instead of reflowing the entire
// file and drowning the actual diff.
var widths = []int{20, 11, 16, 18, 9, 9, 12}

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
		return "ja"
	}
	return "nein"
}

func filterLabel(f policy.WishFilter) string {
	switch f.Scope {
	case policy.WishScopeAll:
		return "alle"
	case policy.WishScopeOwn:
		return "nur eigene"
	case policy.WishScopeNone:
		return "kein Zugriff"
	default:
		return fmt.Sprintf("?? %s", f.Scope)
	}
}
