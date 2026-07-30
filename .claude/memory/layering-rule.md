---
name: layering-rule
description: Nur internal/store darf pgx importieren — warum, und was zu tun ist wenn der Test rot wird
metadata:
  type: project
---

`internal/arch/layering_test.go` behauptet: **kein Paket außerhalb `internal/store`
importiert `pgx` oder `database/sql`.**

## Warum

Die Sichtbarkeitsregel („Wünsche sind bis zum Stichtag vertraulich") wird in
`internal/policy` formuliert und in `internal/store` auf jede Query angewandt. Käme irgendein
anderes Paket direkt an die Datenbank, müsste *es* daran denken, den Filter anzulegen.

Eine vergessene Aufrufstelle ist hier kein Bug, sondern genau das politische Versagen, das
das Projekt verhindern soll. Und die gefährlichen Aufrufstellen sind die, die es noch gar
nicht gibt: CSV-/PDF-Exporte, Mail-Digests, Statistik-Endpunkte. Genau dort hat das
Schwesterprojekt direkte DB-Handler angesammelt.

Der Test kostet ~100 Zeilen und macht aus einer Absicht eine Zwangsläufigkeit.

## Wenn der Test rot wird

**Die Lösung ist eine neue Query in `internal/store`, nicht ein weiterer Eintrag in
`storeOnly`.** Wer die Ausnahmeliste erweitert, hebt die Regel auf.

Falls doch einmal ein legitimer Grund auftaucht (ein Migrationswerkzeug, ein Admin-Kommando):
Eintrag hinzufügen **und** hier notieren, warum — sonst weiß in einem halben Jahr niemand
mehr, ob das Absicht war.

## Umsetzung

Nutzt `go list -json ./...` per `os/exec` statt `golang.org/x/tools/go/packages` — dieselbe
Antwort ohne zusätzliche Abhängigkeit. Prüft `Imports` und `TestImports`, damit auch ein
Testhelfer nicht am Store vorbeigreift.

Läuft in der normalen CI-Testrunde mit, braucht keine Datenbank.
