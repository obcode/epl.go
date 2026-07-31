---
name: identity-and-auth
description: Was der erste Implementierungsschritt gebaut hat — Migration, principal, auth, policy — und welche Entscheidungen darin stecken
metadata:
  type: project
---

Gebaut am 2026-07-31 als erster Fachschritt, auf dem Branch `feat/identity-and-authorization`.
Umsetzung von [[auth-dual-path]] und [[visibility-policy]].

## Was existiert

| Paket / Datei | Inhalt |
| --- | --- |
| `db/migrations/20260731180000_people_roles_tokens.sql` | `person`, `person_role`, `personal_access_token` |
| `db/queries/{person,token}.sql` | sqlc — Lookups je **ein** Roundtrip inkl. Rollen |
| `internal/principal` | `Actor` im Context, stdlib + uuid |
| `internal/auth` | zwei Authentifikatoren, eine Middleware, `auth.mode`, PAT-Format |
| `internal/policy` | Rollen, Phasen, Wunsch-Sichtbarkeit in beiden Formen + Golden-Matrix |
| `internal/store/directory.go` | Adapter auf die auth-Seams |
| `graph/person.graphqls` | `Role`, `Person`, `me` |

**Bewusst nicht dabei:** Module, Instanzen, Wünsche. Der Instanz-Primärschlüssel ist laut
[[open-questions]] blockierend ungeklärt, und eine freigegebene Migration wird nicht mehr
geändert.

## Entscheidungen, die keine Mechanik sind

- **Ein PAT sieht nie einen fremden unveröffentlichten Wunsch**, auch nicht für Planende.
  Eigene Wünsche schon — das sind ihre Daten. Begründung: ein Token ist langlebig, steckt in
  einem Skript und entkoppelt „wer hat das gesehen" von einem Login.
- **ADMIN ist kein Wunsch-Leser.** Betreiben ≠ Planen. Wer wirklich hineinsehen muss, bekommt
  sichtbar DEANS_OFFICE. Steht so in der Golden-Matrix und ist am 27./28.10. diskutierbar.
- **Der Dev-User hat *alle* Rollen**, nicht nur ADMIN. Mit ADMIN allein sieht man in der GUI
  fast nichts, und der Reflex wäre dann, ADMIN zu erweitern — genau das darf nicht passieren.
  Wer eine einzelne Rolle testen will, seedet eine Person und schickt `X-Remote-User`.
- **`off-token` mountet `/api/graphql` gar nicht** (404 statt 401). Ein Not-Aus, der die Route
  entfernt, hat keinen Codepfad mehr, der sich irren könnte.
- **Rollen kommen bei jedem Request aus der Person**, nie aus der Token-Zeile. Deshalb entwertet
  ein Rollenentzug sofort alle Tokens, ohne eine Token-Zeile anzufassen. Test dafür existiert.
- **Rollen-Grants sind noch ungescoped.** Welche Fachgruppe eine Leitung leitet, wird erst
  beantwortet, wenn Fachgruppen Zeilen sind — dann als **eigene Tabelle**, damit diese
  Migration nie geändert werden muss.

## Statuscodes: 401 ≠ 503

Ein fehlgeschlagener Lookup (DB weg) ist **503**, nie 401. Sonst rotiert eine Kollegin ein
Token, das nie kaputt war. Kein Credential ist gar kein Fehler → anonym weiterlaufen lassen,
weil `buildInfo` vor jeder Session antworten muss.

Unbekannte Token-ID und falsches Secret ergeben **dieselbe** Meldung, und der Miss-Pfad hasht
und vergleicht trotzdem (`dummyHash`) — sonst sind die beiden Fälle zeitlich unterscheidbar.
Abgelaufen/widerrufen darf konkret sein: dorthin kommt nur, wer das Secret hat.

## Drei Listen, die auseinanderlaufen können

Rollen stehen im GraphQL-Schema, in `internal/policy` und in der CHECK-Constraint. Die drei
können sich nicht gegenseitig importieren, deshalb je ein Test:
`graph.TestSchemaAndPolicyAgreeOnRoles` und `store.TestDatabaseAndPolicyAgreeOnRoles`. Der
`Role`-Enum ist per `gqlgen.yml` an `policy.Role` gebunden — das entfernt die dritte Kopie.

## Fallstricke, die Zeit gekostet haben

- **`sqlc.yaml` war nie lauffähig:** `rules:` gehört unter den `sql:`-Eintrag, nicht auf die
  oberste Ebene. Fiel erst auf, als es die erste Query gab. Zusätzlich `analyzer.database:
  false`, sonst braucht `sqlc generate` eine laufende Datenbank.
- **`pg_constraint` kennt sqlc nicht** — Katalog-Queries von Hand über den Pool, und **auf
  `current_schema()` einschränken**: parallele Tests haben je eine gleichnamige Constraint,
  und ein ungefilterter Lookup stirbt mit „could not open relation with OID …", während ein
  anderes Schema gerade gedroppt wird.
- **Persona-Tokens hatten alle dieselbe Token-ID** (16 A's). Die ID ist Primärschlüssel — die
  Besetzung ließ sich nicht gemeinsam in ein Schema seeden. Jetzt A…E, Secret beginnt mit
  `example`, was die gitleaks-Allowlist ebenso deckt.
- **Ein abgelaufenes Token lässt sich nicht direkt einfügen** (`CHECK expires_at >
  created_at`). `storetest.SeedToken` schiebt deshalb beide Zeitstempel nach hinten — ein
  abgelaufenes Token ist eines, das früher angelegt wurde und seitdem abgelaufen ist.
- **`$2 - interval '1 hour'`** in einer Query braucht `$2::timestamptz`, sonst leitet Postgres
  den Parameter als `interval` her.
