---
name: scope-enforcement
description: Wie @scope durchgesetzt wird, welche vier Entscheidungen darin stecken, und was noch fehlt
metadata:
  type: project
---

Gebaut am 2026-08-02 auf `feat/scope-directive`. Damit ist der mittlere Term von

> effective permission = (Rolle) ∩ (Scopes) ∩ (Kind)

zum ersten Mal implementiert und nicht nur beschrieben. Vorher trug `principal.Actor.Scopes`
die Liste durch den ganzen Stack, die Spalte existierte, und **niemand las beides**.

## Wo was liegt

| Datei | Inhalt |
| --- | --- |
| `internal/policy/scope.go` | `ScopeArea`, `ScopeVerb`, `Scope`, `ParseScope`, `ScopesAllow`, `ScopeFallback` |
| `graph/directives.graphqls` | Deklaration `@scope(area:, verb:)` + beide Enums |
| `graph/scope.go` | Root-Feld-Walk und `EnforceScopes` (AroundOperations) |
| `gqlgen.yml` | `directives.scope.skip_runtime: true`, Enum-Bindung an die policy-Typen |
| `graph/scope_test.go` | die Schema-weiten CI-Tests |
| `bootstrap/scope_test.go` | Durchsetzung durch beide Türen, mit geseedeten Scopes |

Flächen sind `PUBLIC`, `PROFILE`, `TOKENS`, `ADMIN`. **Neue Flächen kommen mit den Feldern, die
sie brauchen** — eine Fläche ohne Feld dahinter ist ein Versprechen im Enum, das Kolleg:innen
per Introspection lesen können.

## Die vier Entscheidungen

**1. `skip_runtime`, also *keine* generierte Direktive.** Genau andersherum als bei
`@interactiveOnly`, und aus zwei Gründen, die beide die *Operation* betreffen und nicht das
Feld: eine Anfrage nach drei Root-Feldern, von denen zwei erlaubt sind, darf keines ausführen;
und die Strukturregel „eine Mutation ist ein Schreibzugriff" hat kein Feld, an dem sie hängen
könnte. Preis: das Vergessen der Verdrahtung bricht *nicht* laut, anders als bei
`@interactiveOnly`. Dagegen steht `bootstrap/scope_test.go`, das den zusammengebauten Handler
fährt.

**2. Die Strukturregel kommt aus dem Operationstyp, nie aus dem Walk.** Das Schwesterprojekt
leitet „ist das schreibend" aus einem Walk ab, der Fragmente nicht verfolgt — deshalb liest
sich `mutation { ... on Mutation { … } }` dort als nicht datenverändernd. Hier folgt der Walk
Fragmenten **und** das Verb kommt unabhängig davon aus `op.Operation`. Keine der beiden Hälften
trägt allein.

**3. Alles Unbestimmbare wird zu `ADMIN:WRITE`** — kein Feld ohne Annotation, kein
unparsebares Argument, kein Walk, der nichts verstanden hat. Das ist das Netz, nicht der Plan:
`TestEveryRootFieldDeclaresAScope` macht den Build rot. **Das ist der eigentliche Ertrag des
Schritts** — die Felder für Bedarf, Wünsche und Statistik existieren noch nicht, und sie werden
von jemandem geschrieben, der an die Fachlichkeit denkt und nicht an Scopes.

**4. Eine leere Scope-Liste heißt *unbeschränkt*,** innerhalb der Rollen des Besitzers. Dasselbe
Argument wie bei `policy.Narrow`: der Mechanismus kann nur wegnehmen, also muss „nichts
ausgewählt" „nichts weggenommen" heißen. Die andere Lesart hätte beim Deploy jedes existierende
Token getötet, weil die Spalte per Default leer ist.

## Was daraus folgt: der Mint-Pfad ist die offene Flanke

`createPersonalAccessToken` kann **keine Scopes wählen**. Jedes existierende Token trägt also
eine leere Liste, und der Term beißt außerhalb der Tests nirgends. Die Durchsetzung ist scharf,
das Werkzeug zum Verengen fehlt. Nächster Schritt wäre: `scopes:`-Argument an der Mutation,
Validierung in `internal/domain`, Checkbox-Liste in `tallox.gui/konto/tokens`.

Wer das baut, entscheidet dabei implizit über die Richtung von (4): sobald man Scopes wählen
kann, ist „leer" eine bewusste Angabe und nicht mehr nur der Default.

## Introspection meldet keine Direktiven-*Verwendungen*

Nur die Deklaration. Ein per `get-graphql-schema` geholtes Schema — also `tallox.gui/schema.graphql`
und alles, was Kolleg:innen mit Codegen sehen — enthält die `@scope`-Definition und **keine
einzige Annotation**. Erst nach dem Schreiben aufgefallen, an einem Satz in der
Direktiven-Beschreibung, der genau das versprach.

Behelf ist Prosa, wie bei `@interactiveOnly` auch, aber vom anderen Ende her: `@interactiveOnly`
steht in der Beschreibung des betroffenen Feldes, `@scope` in der Beschreibung der Fläche —
jeder `ScopeArea`-Wert zählt seine Felder auf. `TestScopeAreasListTheirFields` vergleicht diese
Listen in beide Richtungen gegen die Annotationen, damit die Prosa nicht driftet.

`tallox.gui/src/lib/schemaDoc.ts` kannte diese Grenze schon für `@interactiveOnly`; der Kommentar
dort nennt jetzt beide.

## Kleinkram, der Zeit kosten würde

- **`gqlparser.LoadQuery` ist deprecated** (staticcheck SA1019 in der CI):
  `LoadQueryWithRules(schema, query, rules.NewDefaultRules())`.
- **Im Test muss validiert und nicht nur geparst werden.** `ast.Field.Definition` setzt der
  Validator, und `requiredScopes` liest genau das. Ein Test, der nur parst, landet versehentlich
  in den Fail-closed-Zweigen und beweist nichts über den Normalpfad.
- **`git checkout <datei>` holt den *Commit*-Stand**, nicht den Stand von vor fünf Minuten. Beim
  Gegenprüfen eines Tests (Annotation entfernen → rot?) gehen damit ungespeicherte Änderungen an
  derselben Datei verloren. Kopie im Scratchpad statt `git checkout`.
- Reihenfolge der Absagen: `INSUFFICIENT_SCOPE` gewinnt gegen `INTERACTIVE_ONLY`, weil
  AroundOperations vor jedem Resolver läuft. Per Test festgenagelt, damit Clients sich darauf
  verlassen können.
