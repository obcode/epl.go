package policy

// Phase is where a semester stands in the planning process.
//
// Two properties are load-bearing and neither is obvious from the type:
//
// The phase is stored on the semester row and advanced by an audited mutation — never derived
// from the calendar. Dates slip, and a process that silently moves on because a Tuesday
// arrived is a process nobody trusts. It also makes the switch a visible artefact at the
// faculty retreat: somebody flips it, and everyone can see who and when.
//
// The phase is separate from wishes_published_at. It is tempting to fold them together —
// "publication happens when the Wunschphase ends" — but the process needs both halves
// independently: the Wunschphase can close without publishing (so late entries stop while the
// planners work), and publication can happen while the Zuteilung is already running. Folding
// them would make one of those two impossible, and it is not knowable today which one the
// faculty will need first.
type Phase string

const (
	// PhaseBedarfsplanung is when the Studiengangsleitungen declare which instances are needed.
	PhaseBedarfsplanung Phase = "BEDARFSPLANUNG"
	// PhaseWunschphase is when lecturers register interest. Entries are confidential — that is
	// the point of the phase, not a property of it.
	PhaseWunschphase Phase = "WUNSCHPHASE"
	// PhaseZuteilung is when the Fachgruppenleitungen fill the instances.
	PhaseZuteilung Phase = "ZUTEILUNG"
	// PhaseFinal is when the plan stands. Changes from here on are corrections, not planning.
	PhaseFinal Phase = "FINAL"
)

// AllPhases returns the phases in process order.
//
// The order is the process, so Before and After below derive from this slice rather than from
// a separately maintained rank — one list to get wrong instead of two.
func AllPhases() []Phase {
	return []Phase{
		PhaseBedarfsplanung,
		PhaseWunschphase,
		PhaseZuteilung,
		PhaseFinal,
	}
}

// ParsePhase turns a stored phase string into a Phase.
//
// Unlike an unknown role, an unknown phase is not something a rule can shrug off: it means
// the semester row says something this code cannot act on. Callers get the false and are
// expected to treat it as an error rather than substituting a default — a default here would
// most likely be BEDARFSPLANUNG, which is the most permissive phase for writes.
func ParsePhase(s string) (Phase, bool) {
	for _, p := range AllPhases() {
		if string(p) == s {
			return p, true
		}
	}
	return "", false
}

// Valid reports whether p is one of the known phases.
func (p Phase) Valid() bool {
	_, ok := ParsePhase(string(p))
	return ok
}

// index is the position in the process, or -1 for an unknown phase.
func (p Phase) index() int {
	for i, known := range AllPhases() {
		if known == p {
			return i
		}
	}
	return -1
}

// Before reports whether p comes earlier in the process than other.
//
// An unknown phase is neither before nor after anything: comparing against a value that is
// not in the process cannot produce a meaningful answer, and returning one would let a typo
// in the database decide whether a deadline has passed.
func (p Phase) Before(other Phase) bool {
	pi, oi := p.index(), other.index()
	return pi >= 0 && oi >= 0 && pi < oi
}

// After reports whether p comes later in the process than other.
func (p Phase) After(other Phase) bool {
	pi, oi := p.index(), other.index()
	return pi >= 0 && oi >= 0 && pi > oi
}
