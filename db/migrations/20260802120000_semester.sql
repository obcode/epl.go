-- Migration 4: the semester, and the phase the planning of it is in.
--
-- The first table of the domain proper, and deliberately the only one. Modules and instances
-- still wait on the instance primary key — module, module version, module + programme,
-- module + programme + course type — which is the most expensive open question in the project
-- and is not answered yet. A released migration is never edited, so writing those tables now
-- would freeze a guess.
--
-- The semester does not depend on that answer, and everything that comes after it does depend
-- on the semester: demand, wishes, assignment and the statistics all hang off one.
--
-- +goose Up

CREATE TABLE semester (
    -- A surrogate key, even though `code` below is unique and never changes.
    --
    -- This is the lesson from the sibling project written into a schema: there, a natural
    -- identifier became the primary key, every downstream table inherited its quirks, and
    -- correcting one of them stopped being possible. A uuid costs 16 bytes and keeps the
    -- question "what is a semester called" separate from the question "what do other rows
    -- point at".
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The natural key, in the form 2027S or 2026W: four digits and a letter.
    --
    -- The letter is the term and the digits are the year the term *starts* in, so the winter
    -- semester 2026/27 is 2026W. Spelling it out that way is what makes the code unambiguous
    -- for a winter semester, which spans two calendar years and is therefore the case a
    -- year-only column gets wrong.
    --
    -- Two properties are worth the constraint below. It sorts chronologically as plain text —
    -- 2026W < 2027S < 2027W, because the year leads and S precedes W within a year, which is
    -- also the order the terms happen in. And it is short enough to appear in a URL, in an
    -- export filename and in a colleague's evaluation script without anybody reaching for a
    -- uuid.
    --
    -- German names ("Sommersemester 2027") are the GUI's business. The translation happens
    -- once, where the people who read it are.
    code                text NOT NULL UNIQUE,

    -- Where this semester stands in the process.
    --
    -- In the database and switched by an audited mutation, never derived from the calendar.
    -- Dates slip, and a process that moves on because a Tuesday arrived is a process nobody
    -- trusts. It also makes the switch a visible act: somebody flips it, and everyone can see
    -- who and when.
    phase               text NOT NULL DEFAULT 'DEMAND_PLANNING',

    -- When the wishes of this semester became visible to everybody, or NULL while they are
    -- still confidential. The rule in internal/policy is exactly `IS NOT NULL`.
    --
    -- Deliberately *not* folded into the phase, and there is no constraint tying the two
    -- together. The process needs both halves independently: the wish phase can close without
    -- publishing, so that late entries stop while the planners work, and publication can
    -- happen while the assignment is already running. A constraint saying "only publishable in
    -- WISHES" would make one of those impossible, and it is not knowable today which one the
    -- faculty will need first.
    --
    -- There is no un-publishing, and that is a property of the world rather than of this
    -- column: once colleagues have seen each other's entries, a NULL here would only be a lie
    -- about it.
    wishes_published_at timestamptz,

    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    -- Four digits and S or W. Enforced here rather than only in Go, because the chronological
    -- sort above is a promise made to every ORDER BY in the system, and a single row reading
    -- "SS2027" would quietly break it for the whole table.
    CONSTRAINT semester_code_is_year_and_term CHECK (code ~ '^[0-9]{4}[SW]$'),

    -- The phases, as internal/policy knows them. Three places hold this list — the schema, the
    -- policy and this constraint — and they cannot import one another, so
    -- store.TestDatabaseAndPolicyAgreeOnPhases compares this one against the policy.
    CONSTRAINT semester_phase_is_known CHECK (
        phase IN ('DEMAND_PLANNING', 'WISHES', 'ASSIGNMENT', 'FINAL')
    )
);

-- The list in the GUI and every "which semesters are there" query, newest first. The index is
-- on the natural key because that is what sorts chronologically; sorting by created_at would
-- order them by when somebody got round to entering them.
CREATE INDEX semester_code_desc_idx ON semester (code DESC);

-- +goose Down

DROP TABLE semester;
