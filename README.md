# timesheet-cli

A cross-platform command-line client for managing entries and inspecting invoices in [Luby Timesheet](https://luby-timesheet.azurewebsites.net). It authenticates directly against the site's HTTP endpoints; no browser or language runtime is needed after installation.

## Installation

On macOS or Linux, install the latest release with:

```sh
curl -fsSL https://raw.githubusercontent.com/gustmrg/timesheet-cli/main/scripts/install.sh | sh
```

The script detects your operating system and architecture, verifies the release checksum, and installs `timesheet` to `/usr/local/bin`. Set `INSTALL_DIR` to choose another location, or `VERSION` to install a specific release:

```sh
curl -fsSL https://raw.githubusercontent.com/gustmrg/timesheet-cli/main/scripts/install.sh | INSTALL_DIR="$HOME/.local/bin" sh
curl -fsSL https://raw.githubusercontent.com/gustmrg/timesheet-cli/main/scripts/install.sh | VERSION=0.5.0 sh
```

For a private repository, export a fine-grained token with read-only Contents access to this repository (a classic token needs the `repo` scope). The token authenticates both the initial script request and the script's release API requests:

```sh
export GH_TOKEN="..."
curl -fsSL \
  -H "Authorization: Bearer $GH_TOKEN" \
  -H "Accept: application/vnd.github.raw+json" \
  "https://api.github.com/repos/gustmrg/timesheet-cli/contents/scripts/install.sh?ref=main" | sh
```

`GITHUB_TOKEN` is also accepted when `GH_TOKEN` is unset. Avoid saving tokens in shell history or files, and unset the token when finished.

Windows users can download the appropriate archive from [GitHub Releases](https://github.com/gustmrg/timesheet-cli/releases), extract it, and place `timesheet.exe` on their `PATH`.

Developers with Go 1.26 or later can install from source:

```sh
go install github.com/gustmrg/timesheet-cli@latest
```

Verify the installation:

```sh
timesheet --version
timesheet --help
```

## Upgrading

Upgrade the installed executable to the latest GitHub release:

```sh
timesheet upgrade
```

The command downloads the archive for the current operating system and architecture, verifies it against the release's `checksums.txt`, and replaces the running executable. If the repository is private, export `GH_TOKEN` or `GITHUB_TOKEN` with read-only Contents access before running it. The executable's directory must be writable; otherwise, rerun the installer with a writable `INSTALL_DIR` or execute the update with the required operating-system permissions.

Use `--json` to receive the current version, latest version, update status, and executable path in a structured response.

## Authentication

Authenticate interactively before using the other commands:

```sh
timesheet login
```

The session is stored at `~/.timesheet-cli/session.json`. Existing sessions created by the earlier TypeScript version remain compatible. Authentication cookies renewed by the server are saved automatically.

To allow the CLI to reauthenticate after the server fully expires a session, explicitly save the credentials in the operating system's credential vault:

```sh
timesheet login --save-credentials
```

This uses macOS Keychain, Windows Credential Manager, or the Secret Service API on Linux. For a permanently headless Linux system without Secret Service, explicitly select the protected file store instead:

```sh
timesheet login --save-credentials --credential-store file
```

The CLI creates `~/.timesheet-cli/credentials.json` automatically, creates its parent directory with mode `0700`, and writes the file atomically with mode `0600`. The file contains plaintext credentials protected by Unix ownership and permissions; anyone with root access or access to the account can read it. The CLI never silently falls back from the system vault to file storage.

Safe read operations reauthenticate and retry once. Add, update, and delete requests are never automatically retried. If a write encounters an expired session, the CLI renews the session when possible and asks you to rerun the command.

Credentials may also come from flags or environment variables:

```sh
timesheet login --user YOUR_LOGIN --pass YOUR_PASSWORD
TIMESHEET_USER=YOUR_LOGIN TIMESHEET_PASS=YOUR_PASSWORD timesheet login
```

Interactive entry is preferable because `--pass` can expose the password in shell history and process listings, while environment variables can leak through logs or process inspection.

Log out while retaining saved credentials, or remove both forms of authentication:

```sh
timesheet logout
timesheet logout --forget-credentials
```

## Commands

```text
timesheet login [--user LOGIN] [--pass PASSWORD] [--save-credentials] [--credential-store system|file]
timesheet logout [--forget-credentials]
timesheet list [--limit N] [--all]
timesheet meta
timesheet status ENTRY_ID
timesheet invoice list [--limit N] [--offset N] [--search TEXT] [--status pending|sent] [--all]
timesheet invoice preview PROCESS_ID
timesheet add --customer CUSTOMER --project PROJECT --category CATEGORY \
  [--date DATE] --start HH:mm --end HH:mm --desc DESCRIPTION [--not-monetize]
timesheet update ENTRY_ID [entry flags] [--monetize | --not-monetize]
timesheet delete ENTRY_ID [--yes]
timesheet upgrade
timesheet version
```

Every command supports `-h`/`--help`. `--json` is a global flag and may appear before or after a subcommand.

Customer, project, and category values accept a numeric ID or a unique, case-insensitive name substring. Dates accept `YYYY-MM-DD` or `dd/MM/yyyy`; omitted add dates default to today. Times must use 24-hour `HH:mm` format. Descriptions are always treated as plain text and safely encoded for the server.

Examples:

```sh
timesheet list --limit 5
timesheet meta --json
timesheet status 12345
timesheet invoice list --status pending
timesheet invoice list --search "developer name" --limit 10
timesheet invoice preview 901 --json
timesheet upgrade --json

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

`delete` asks for confirmation unless `--yes` is supplied. `timesheet update ENTRY_ID` updates a timesheet entry, while `timesheet upgrade` upgrades the CLI itself. For entry updates, `--monetize` and `--not-monetize` explicitly set the value; omitting both preserves the current value.

## JSON output

Successful commands return an envelope on stdout:

```json
{"ok":true,"data":{}}
```

Successful commands may include nonfatal warnings:

```json
{"ok":true,"data":{},"warnings":[{"code":"session_persist_failed","message":"..."}]}
```

Failures return an envelope on stderr and exit nonzero:

```json
{"ok":false,"error":{"code":"invalid_input","message":"..."}}
```

Stable error codes are `usage`, `invalid_input`, `auth_required`, `login_failed`, `credential_store_error`, `not_found`, `ambiguous_value`, `network_error`, `invalid_server_response`, `operation_failed`, `upgrade_failed`, and `internal_error`. Stable warning codes are `credential_store_unavailable` and `session_persist_failed`.

Read-command data shapes:

- `list`: `entries`, `returned`, and `total`
- `meta`: nested `customers`, `projects`, and `categories`
- `status`: `entryId`, normalized `state`, `manager`, and `created`
- `invoice list`: normalized `invoices` plus `offset`, `returned`, `total`, and `filtered`
- `invoice preview`: `processId` and project-level `items` with monetized/non-monetized hours and amounts
- `upgrade`: `currentVersion`, `latestVersion`, `updated`, and `executable`

Write commands return the action and normalized entry data; delete returns the entry ID and whether deletion occurred.

Invoice commands are read-only. Invoice status is normalized to `pending` or `sent`; `--status` filters that normalized status, while `--search` uses the server's invoice search. Use `--offset` with `--limit` for pagination or `--all` to return every matching invoice.

## Configuration

| Variable | Purpose | Default |
| --- | --- | --- |
| `TIMESHEET_USER` | Login username | Interactive prompt |
| `TIMESHEET_PASS` | Login password | Interactive prompt |
| `TIMESHEET_SESSION` | Session file location | `~/.timesheet-cli/session.json` |
| `TIMESHEET_CREDENTIALS` | File credential store location | `~/.timesheet-cli/credentials.json` |
| `TIMESHEET_BASE_URL` | Timesheet server URL | `https://luby-timesheet.azurewebsites.net` |
| `GH_TOKEN` | GitHub token used by CLI upgrade | `GITHUB_TOKEN`, then unauthenticated |
| `GITHUB_TOKEN` | Fallback GitHub token used by CLI upgrade | Unauthenticated |

The session and credentials files contain authentication secrets. Do not print, share, or commit them. Credentials saved with `--save-credentials` are scoped to the configured server origin; the `system` store uses the operating system vault, while the explicitly selected `file` store relies on file ownership and mode `0600`.

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
