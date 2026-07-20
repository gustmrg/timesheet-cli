# timesheet-cli

A command-line client for managing entries in [Luby Timesheet](https://luby-timesheet.azurewebsites.net). Use it to authenticate, list and inspect entries, view available customers and projects, and create, update, or delete timesheet records.

## Requirements

- Node.js 24 or later
- npm
- A valid Luby Timesheet account

Node.js runs the TypeScript source directly, so there is no separate build step.

## Installation

Clone the repository, install its dependencies, and link the `timesheet` command:

```sh
git clone https://github.com/gustmrg/timesheet-cli.git
cd timesheet-cli
npm install
npm link
```

Verify the installation:

```sh
timesheet --help
```

If you do not want to install the command globally, replace `timesheet` in the examples below with `node ./timesheet.ts`.

## Getting started

First, authenticate with your Luby Timesheet credentials:

```sh
timesheet login
```

The command prompts for your username and password, then saves the session cookies to `~/.timesheet-cli/session.json`. Run `timesheet login` again whenever the session expires.

You can also provide credentials with flags or environment variables:

```sh
timesheet login --user YOUR_LOGIN --pass YOUR_PASSWORD

TIMESHEET_USER=YOUR_LOGIN TIMESHEET_PASS=YOUR_PASSWORD timesheet login
```

The interactive prompt or environment variables are preferable to `--pass`, which may expose the password in your shell history or process list.

After logging in, view your recent entries:

```sh
timesheet list
```

## Commands

### `login`

Authenticate and save a local session.

```sh
timesheet login [--user LOGIN] [--pass PASSWORD]
```

If a flag is omitted, the command uses `TIMESHEET_USER` or `TIMESHEET_PASS` when set, then falls back to an interactive prompt.

### `list`

List timesheet entries, most recent first. By default, the command shows the latest 20 entries.

```sh
timesheet list [--limit N] [--all] [--json]
```

- `--limit N` shows at most `N` entries.
- `--all` shows every entry and takes precedence over `--limit`.
- `--json` prints machine-readable JSON instead of a table.

Examples:

```sh
timesheet list --limit 5
timesheet list --all
timesheet list --json
```

### `meta`

List the customers, projects, and categories available to your account. This is useful for finding names or numeric IDs before adding an entry.

```sh
timesheet meta [--json]
```

### `status`

Show the evaluation status of an entry.

```sh
timesheet status <entry-id> [--json]
```

Use `timesheet list` to find an entry ID.

### `add`

Create a timesheet entry.

```sh
timesheet add \
  --customer CUSTOMER \
  --project PROJECT \
  --category CATEGORY \
  [--date DATE] \
  --start HH:mm \
  --end HH:mm \
  --desc "DESCRIPTION" \
  [--not-monetize]
```

`--date` defaults to today. Customer, project, and category values may be numeric IDs or unique, case-insensitive name substrings.

Example:

```sh
timesheet add \
  --customer ernst \
  --project internal \
  --category daily \
  --date 2026-07-20 \
  --start 09:00 \
  --end 10:00 \
  --desc "Daily meeting"
```

If a name matches more than one option, the command reports that it is ambiguous. Use `timesheet meta` to find a more specific name or its numeric ID.

### `update`

Update an existing entry. Only the fields you provide are changed; all other fields keep their current values.

```sh
timesheet update <entry-id> \
  [--customer CUSTOMER] \
  [--project PROJECT] \
  [--category CATEGORY] \
  [--date DATE] \
  [--start HH:mm] \
  [--end HH:mm] \
  [--desc "DESCRIPTION"] \
  [--not-monetize]
```

Example:

```sh
timesheet update 12345 --end 18:00 --desc "Updated description"
```

### `delete`

Delete an entry. The command asks for confirmation unless `--yes` is supplied.

```sh
timesheet delete <entry-id> [--yes]
```

Example:

```sh
timesheet delete 12345
timesheet delete 12345 --yes
```

## Value formats

- Dates accept `YYYY-MM-DD` or `dd/MM/yyyy`. When omitted while adding an entry, the date defaults to today.
- Times use the 24-hour `HH:mm` format.
- `--customer`, `--project`, and `--category` accept either a numeric ID or a unique, case-insensitive substring of the name.
- Descriptions are provided as plain text.

## Help

The CLI supports the following top-level help commands:

```sh
timesheet --help
timesheet help
```

The short `-h` flag and command-specific help such as `timesheet add --help` are not currently supported.

## Configuration

The following environment variables are available:

| Variable | Purpose | Default |
| --- | --- | --- |
| `TIMESHEET_USER` | Login username | Interactive prompt |
| `TIMESHEET_PASS` | Login password | Interactive prompt |
| `TIMESHEET_SESSION` | Session file location | `~/.timesheet-cli/session.json` |
| `TIMESHEET_BASE_URL` | Timesheet server URL | `https://luby-timesheet.azurewebsites.net` |

The session file contains active authentication cookies. The CLI creates it with permissions restricted to the current user. To use a different location, set `TIMESHEET_SESSION` to an absolute file path.

## Development

Run the type checker with:

```sh
npm run typecheck
```

The project uses strict TypeScript settings and Node.js native type stripping, so the CLI runs directly from `timesheet.ts` without generating JavaScript files.
