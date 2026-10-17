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

Both installers end with separate optional prompts for the bundled agent skill (`skills/timesheet-cli/SKILL.md`): one for the global agents directory (`~/.agents/skills`) and another for Claude (`~/.claude/skills`). Answer `y` to either prompt to install it in that location or press Enter to skip.

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
timesheet mcp [--http ADDRESS] [--token-file PATH]
timesheet mcp install [--http ADDRESS] [--token-file PATH]
timesheet mcp uninstall
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

## MCP server

`timesheet mcp` serves the CLI's operations to AI agents over the [Model Context Protocol](https://modelcontextprotocol.io). The agent gets tools; the session and saved credentials stay with the CLI and never reach it.

| Tool | Equivalent command |
| --- | --- |
| `list_entries` | `timesheet list` |
| `get_metadata` | `timesheet meta` |
| `get_entry` | `timesheet show` |
| `get_entry_status` | `timesheet status` |
| `list_invoices` | `timesheet invoice list` |
| `preview_invoice` | `timesheet invoice preview` |
| `create_entry` | `timesheet add` |
| `update_entry` | `timesheet update` |
| `delete_entry` | `timesheet delete --yes` |

Read tools are annotated read-only and the write tools destructive, so clients that ask before each call can allow reads and keep asking for writes. Results use the same envelope as `--json`. `upgrade`, `login`, and `logout` are not exposed. Log in once with `timesheet login --save-credentials` (add `--credential-store file` on a headless machine) so the server can renew its session.

Without flags the server speaks MCP over stdin and stdout, for clients that launch it as a local process:

```json
{ "mcpServers": { "timesheet": { "command": "timesheet", "args": ["mcp"] } } }
```

With `--http` it serves Streamable HTTP at `/mcp`:

```sh
timesheet mcp --http 127.0.0.1:8765
```

Every HTTP request must send `Authorization: Bearer <token>`. The token is read from `TIMESHEET_MCP_TOKEN`, or from `~/.timesheet-cli/mcp-token`, which is created with a random token (mode `0600`) on first start; `--token-file` names another file, which must already exist. Tokens must be at least 32 characters.

### Running it as a service on Linux

To keep the HTTP server running in the background, install it as a systemd user service:

```sh
timesheet login --save-credentials --credential-store file
timesheet mcp install                 # listens on 127.0.0.1:8765; --http changes it
loginctl enable-linger "$USER"        # keep it running while you are logged out
```

`install` writes `~/.config/systemd/user/timesheet-mcp.service` pointing at the running executable, creates the token file if needed, and enables and starts the service, which then starts again at login and after a crash. Running it again replaces the service and restarts it. `TIMESHEET_BASE_URL`, `TIMESHEET_SESSION`, and `TIMESHEET_CREDENTIALS` are copied into the service when set; the service always reads its token from the file, never from `TIMESHEET_MCP_TOKEN`. The command warns when lingering is off, because systemd then stops the service when your last session ends.

Use the file credential store on a server: the operating system vault is usually locked or absent while you are logged out, so the service could not renew its session. After `timesheet upgrade`, run `systemctl --user restart timesheet-mcp` to load the new version. Inspect the service with `systemctl --user status timesheet-mcp` and `journalctl --user -u timesheet-mcp`, and remove it with `timesheet mcp uninstall`, which keeps the token file, session, and credentials.

The service is Linux only. On macOS or Windows, run `timesheet mcp --http ADDRESS` under launchd, Task Scheduler, or another service manager.

### Connecting over HTTPS

Clients that require HTTPS, such as remote MCP servers in Wisp Bot, can reach it through a TLS reverse proxy. On a machine in a Tailscale tailnet:

```sh
tailscale serve --bg --https=8443 http://127.0.0.1:8765
```

Then add `https://<machine>.<tailnet>.ts.net:8443/mcp` as a remote MCP server with header authentication: header `Authorization`, value `Bearer <token>`.

## Configuration

| Variable | Purpose | Default |
| --- | --- | --- |
| `TIMESHEET_USER` | Login username | Interactive prompt |
| `TIMESHEET_PASS` | Login password | Interactive prompt |
| `TIMESHEET_SESSION` | Session file location | `~/.timesheet-cli/session.json` |
| `TIMESHEET_CREDENTIALS` | File credential store location | `~/.timesheet-cli/credentials.json` |
| `TIMESHEET_BASE_URL` | Timesheet server URL | `https://luby-timesheet.azurewebsites.net` |
| `TIMESHEET_MCP_TOKEN` | Bearer token for `timesheet mcp --http` | `~/.timesheet-cli/mcp-token` |

The session, credentials, and MCP token files contain authentication secrets. Do not print, share, or commit them.

