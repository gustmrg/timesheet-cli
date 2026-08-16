# timesheet-cli

A cross-platform command-line client for managing entries and inspecting invoices in [Luby](https://github.com/lubysoftware) Timesheet. It authenticates directly against the site's HTTP endpoints; no browser or language runtime is needed after installation.

## Installation

On macOS or Linux, install the latest release to `/usr/local/bin` with:

```sh
curl -fsSL https://raw.githubusercontent.com/gustmrg/timesheet-cli/main/scripts/install.sh | sh
```

The script detects your operating system and architecture and verifies the release checksum. On Windows, run the PowerShell installer instead; it installs to `%LOCALAPPDATA%\Programs\timesheet` and adds it to your user `PATH`:

```powershell
irm https://raw.githubusercontent.com/gustmrg/timesheet-cli/main/scripts/install.ps1 | iex
```

Both installers end with an optional prompt to install the bundled agent skill (`skills/timesheet-cli/SKILL.md`) into `~/.agents/skills` and `~/.claude/skills`; answer `y` to install it or press Enter to skip.

Verify the installation:

```sh
timesheet --version
timesheet --help
```

## Upgrading

```sh
timesheet upgrade
```

This downloads the latest release for your platform, verifies its checksum, and replaces the running executable.

## Getting started

Log in once, then manage your entries:

```sh
timesheet login

timesheet add \
  --customer ernst \
  --project internal \
  --category daily \
  --start 09:00 \
  --end 10:00 \
  --desc "Daily meeting"

timesheet list
```

The session is stored at `~/.timesheet-cli/session.json`. To let the CLI reauthenticate after the server expires a session, save your credentials in the operating system's credential vault:

```sh
timesheet login --save-credentials
timesheet logout                  # keep saved credentials
timesheet logout --forget-credentials
```

This uses macOS Keychain, Windows Credential Manager, or the Secret Service API on Linux. On a headless Linux system without Secret Service, use `timesheet login --save-credentials --credential-store file` instead; credentials are then kept in `~/.timesheet-cli/credentials.json` with mode `0600`.

## Commands

```text
timesheet login [--user LOGIN] [--pass PASSWORD] [--save-credentials] [--credential-store system|file]
timesheet logout [--forget-credentials]
timesheet list [--limit N] [--all]
timesheet meta
timesheet status ENTRY_ID
timesheet show ENTRY_ID
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

Customer, project, and category values accept a numeric ID or a unique, case-insensitive name substring. Dates accept `YYYY-MM-DD` or `dd/MM/yyyy` and default to today. Times use 24-hour `HH:mm` format.

Examples:

```sh
timesheet list --limit 5
timesheet meta
timesheet status 12345
timesheet invoice list --status pending
timesheet invoice preview 901
timesheet update 12345 --end 18:00 --monetize
timesheet delete 12345 --yes
```

`delete` asks for confirmation unless `--yes` is supplied. `timesheet update ENTRY_ID` updates a timesheet entry, while `timesheet upgrade` upgrades the CLI itself.

## JSON output

With `--json`, successful commands print an envelope on stdout:

```json
{"ok":true,"data":{}}
```

Failures print an envelope on stderr and exit nonzero:

```json
{"ok":false,"error":{"code":"invalid_input","message":"..."}}
```

Stable error codes are `usage`, `invalid_input`, `auth_required`, `login_failed`, `credential_store_error`, `not_found`, `ambiguous_value`, `network_error`, `invalid_server_response`, `operation_failed`, `upgrade_failed`, and `internal_error`.

## Configuration

| Variable | Purpose | Default |
| --- | --- | --- |
| `TIMESHEET_USER` | Login username | Interactive prompt |
| `TIMESHEET_PASS` | Login password | Interactive prompt |
| `TIMESHEET_SESSION` | Session file location | `~/.timesheet-cli/session.json` |
| `TIMESHEET_CREDENTIALS` | File credential store location | `~/.timesheet-cli/credentials.json` |
| `TIMESHEET_BASE_URL` | Timesheet server URL | `https://luby-timesheet.azurewebsites.net` |

The session and credentials files contain authentication secrets. Do not print, share, or commit them.

