# timesheet-cli

A cross-platform command-line client for managing entries in [Luby Timesheet](https://luby-timesheet.azurewebsites.net). It authenticates directly against the site's HTTP endpoints; no browser or language runtime is needed after installation.

## Installation

Download the archive for your operating system and architecture from [GitHub Releases](https://github.com/gustmrg/timesheet-cli/releases), extract it, and place `timesheet` (or `timesheet.exe`) on your `PATH`.

Developers with Go 1.26 or later can install from source:

```sh
go install github.com/gustmrg/timesheet-cli@latest
```

Verify the installation:

```sh
timesheet --version
timesheet --help
```

## Authentication

Authenticate interactively before using the other commands:

```sh
timesheet login
```

The session is stored at `~/.timesheet-cli/session.json`. Existing sessions created by the earlier TypeScript version remain compatible. Run `login` again when the session expires.

Credentials may also come from flags or environment variables:

```sh
timesheet login --user YOUR_LOGIN --pass YOUR_PASSWORD
TIMESHEET_USER=YOUR_LOGIN TIMESHEET_PASS=YOUR_PASSWORD timesheet login
```

Interactive entry is preferable because `--pass` can expose the password in shell history and process listings, while environment variables can leak through logs or process inspection.

## Commands

```text
timesheet login [--user LOGIN] [--pass PASSWORD]
timesheet list [--limit N] [--all]
timesheet meta
timesheet status ENTRY_ID
timesheet add --customer CUSTOMER --project PROJECT --category CATEGORY \
  [--date DATE] --start HH:mm --end HH:mm --desc DESCRIPTION [--not-monetize]
timesheet update ENTRY_ID [entry flags] [--monetize | --not-monetize]
timesheet delete ENTRY_ID [--yes]
timesheet version
```

Every command supports `-h`/`--help`. `--json` is a global flag and may appear before or after a subcommand.

Customer, project, and category values accept a numeric ID or a unique, case-insensitive name substring. Dates accept `YYYY-MM-DD` or `dd/MM/yyyy`; omitted add dates default to today. Times must use 24-hour `HH:mm` format. Descriptions are always treated as plain text and safely encoded for the server.

Examples:

```sh
timesheet list --limit 5
timesheet meta --json
timesheet status 12345

timesheet add \
  --customer ernst \
  --project internal \
  --category daily \
  --date 2026-07-20 \
  --start 09:00 \
  --end 10:00 \
  --desc "Daily meeting"

timesheet update 12345 --end 18:00 --monetize
timesheet delete 12345
```

`delete` asks for confirmation unless `--yes` is supplied. On update, `--monetize` and `--not-monetize` explicitly set the value; omitting both preserves the current value.

## JSON output

Successful commands return an envelope on stdout:

```json
{"ok":true,"data":{}}
```

Failures return an envelope on stderr and exit nonzero:

```json
{"ok":false,"error":{"code":"invalid_input","message":"..."}}
```

Stable error codes are `usage`, `invalid_input`, `auth_required`, `login_failed`, `not_found`, `ambiguous_value`, `network_error`, `invalid_server_response`, `operation_failed`, and `internal_error`.

Read-command data shapes:

- `list`: `entries`, `returned`, and `total`
- `meta`: nested `customers`, `projects`, and `categories`
- `status`: `entryId`, normalized `state`, `manager`, and `created`

Write commands return the action and normalized entry data; delete returns the entry ID and whether deletion occurred.

## Configuration

| Variable | Purpose | Default |
| --- | --- | --- |
| `TIMESHEET_USER` | Login username | Interactive prompt |
| `TIMESHEET_PASS` | Login password | Interactive prompt |
| `TIMESHEET_SESSION` | Session file location | `~/.timesheet-cli/session.json` |
| `TIMESHEET_BASE_URL` | Timesheet server URL | `https://luby-timesheet.azurewebsites.net` |

The session file contains active authentication cookies. Do not print, share, or commit it.

## Development

Requirements: Go 1.26 or later.

```sh
go test -race ./...
go vet ./...
go build ./...
```

The test suite uses local HTTP servers and synthetic HTML fixtures. It never contacts the live timesheet service or reads local sessions and captures.

Version tags matching `v*` trigger GitHub Actions to publish statically linked archives for Linux, macOS, and Windows on `amd64` and `arm64`, together with checksums and Sigstore provenance.

The first Go-based release is `v0.2.0`.
