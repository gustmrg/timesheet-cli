# timesheet-cli

CLI for [luby-timesheet](https://luby-timesheet.azurewebsites.net), derived by
recording an authenticated browser session (`scripts/capture.ts`) and replaying
the app's internal HTTP endpoints directly — no browser needed at runtime.

Written in TypeScript (checked with the Go-native TypeScript 7 compiler).
No build step: Node 24 runs the `.ts` sources directly via type stripping;
`tsc --noEmit` is the typechecker.

## Setup

```sh
npm install
npm link          # optional: puts `timesheet` on your PATH
```

Authenticate (stores session cookies in `session.json`, gitignored):

```sh
timesheet login            # prompts for credentials
```

Re-running `login` is all that's needed when the session expires.

## Usage

```sh
timesheet list [--all] [--limit N] [--json]   # entries, most recent first
timesheet meta [--json]                       # customers / projects / categories
timesheet status <id> [--json]                # evaluation status of an entry

timesheet add --customer C --project P --category K \
              [--date D] --start S --end E --desc "text" [--not-monetize]

timesheet update <id> [--date D] [--start S] [--end E] [--desc T] ...
timesheet delete <id> [--yes]
```

- `--customer/--project/--category` accept a numeric id **or** a name
  substring (case-insensitive), e.g. `--customer ernst --category daily`.
- Dates: `YYYY-MM-DD` or `dd/MM/yyyy` (default: today). Times: `HH:mm`.
- Descriptions are plain text; they're wrapped in `<p>` on the wire.

## How it works

- `scripts/capture.ts` — opens a Playwright browser with a persistent
  profile, logs every request/response to `capture/requests-*.jsonl`
  (plus a HAR) while you browse logged in. Used to derive the endpoint map;
  only needed again if the site's API changes (`npm run capture`).
- `src/api.ts` — HTTP client (cookie jar, manual redirects) implementing:
  - `GET /Worksheet/Read` — full entry list (server-rendered HTML, parsed)
  - `GET /Worksheet/DropDownChange?idcustomer|idproject` — metadata
  - `GET /Worksheet/Update?id` — single entry (for edits)
  - `POST /Worksheet/UpdateMultiple` — create entry (JSON)
  - `POST /Worksheet/UpdateOne` — update entry (JSON)
  - `POST /Worksheet/Delete` — delete entry (form)
  - `POST /Worksheet/ReadEvaluate` — evaluation status (form)
  - `POST /` — credential login (form + `__RequestVerificationToken`)
- `src/parse.ts` — regex parsers for the server-rendered pages.
- `src/jar.ts` — cookie jar persisted to `session.json`.

Auth is plain ASP.NET session cookies; no CSRF token is required on the
worksheet endpoints (observed in the capture).

## Development

```sh
npm run typecheck   # tsc --noEmit (TypeScript 7, Go-native compiler)
```

`tsconfig.json` is strict (`strict`, `noUncheckedIndexedAccess`,
`exactOptionalPropertyTypes`) and sets `erasableSyntaxOnly`, which keeps the
code compatible with Node's built-in type stripping — so the CLI runs as
`.ts` with zero transpile step.

## Files that stay local (gitignored)

- `session.json` — live session cookies
- `capture/` — recorded traffic (contains tokens and your data)
