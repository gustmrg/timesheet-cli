---
name: timesheet-cli
description: Manage Luby Timesheet entries, inspect invoice processes, and upgrade the installed CLI with the local `timesheet` command. Use this skill whenever the user asks to log hours, create or edit a timesheet entry, list recent time records, inspect customers/projects/categories, check approval status, list pending or sent invoices, preview an invoice process, upgrade the timesheet CLI, delete an entry, authenticate the timesheet CLI, or automate Luby Timesheet work—even when they do not explicitly mention this skill or the CLI.
compatibility: Requires the `timesheet` executable on PATH, or Go 1.26+ with a local checkout of timesheet-cli.
---

# Luby Timesheet CLI

Use the `timesheet` CLI to manage the user's Luby Timesheet records. Prefer the CLI over direct HTTP requests because it owns authentication, input normalization, metadata resolution, and server-specific behavior.

## Locate the CLI

First check whether the installed command is available:

```sh
command -v timesheet
```

If it is available, use `timesheet`. When working inside the `timesheet-cli` repository and the installed command is unavailable, use:

```sh
go run .
```

Refer to the selected invocation as `<timesheet>` in the workflow below. Do not install the executable globally unless the user asks for installation.

## Protect authentication

The CLI stores session cookies at `~/.timesheet-cli/session.json` by default. `TIMESHEET_SESSION` can override that location. On explicitly configured headless systems, credentials may be stored at `~/.timesheet-cli/credentials.json`; `TIMESHEET_CREDENTIALS` can override that location.

- Never read, print, copy, summarize, commit, or expose the session or credentials file. They contain authentication secrets.
- Never ask the user to paste a password into the conversation.
- Do not put passwords in command arguments, environment variables, logs, or scripts on the user's behalf.
- The CLI automatically persists sliding cookie renewal. Credentials are stored separately in macOS Keychain, Windows Credential Manager, or Linux Secret Service only when the user runs `<timesheet> login --save-credentials`. Permanently headless systems can explicitly use a mode-`0600` plaintext file with `--credential-store file`; never select it without the user's informed approval.
- Read commands can automatically reauthenticate and retry once when saved credentials are available. Write commands are never automatically retried.
- If authentication is missing and automatic renewal is unavailable, ask the user to run `<timesheet> login` in an interactive terminal. Mention `--save-credentials` only when they want future automatic reauthentication.

## Choose the command

| User intent | Command |
| --- | --- |
| Authenticate | `<timesheet> login --json` |
| Authenticate and opt into automatic renewal | `<timesheet> login --save-credentials --json` |
| Authenticate headlessly with explicit file storage | `<timesheet> login --save-credentials --credential-store file --json` |
| Remove the cookie session | `<timesheet> logout --json` |
| Remove the session and saved credentials | `<timesheet> logout --forget-credentials --json` |
| List recent entries | `<timesheet> list --json` |
| List all entries | `<timesheet> list --all --json` |
| Limit returned entries | `<timesheet> list --limit N --json` |
| Discover customers, projects, and categories | `<timesheet> meta --json` |
| Check an entry's evaluation | `<timesheet> status ID --json` |
| List invoice processes | `<timesheet> invoice list --json` |
| Search invoice processes | `<timesheet> invoice list --search TEXT --json` |
| Filter pending or sent invoices | `<timesheet> invoice list --status pending\|sent --json` |
| Preview an invoice process | `<timesheet> invoice preview PROCESS_ID --json` |
| Upgrade the installed CLI | `<timesheet> upgrade --json` |
| Create an entry | `<timesheet> add ... --json` |
| Change an entry | `<timesheet> update ID ... --json` |
| Delete an entry | `<timesheet> delete ID --json` |

Use `--json` for every command because structured output is safer to interpret than terminal text. Successful responses have the shape `{"ok":true,"data":...}` and may include a top-level `warnings` array. Failures are written to stderr as `{"ok":false,"error":{"code":"...","message":"..."}}`; inspect `data` rather than expecting the payload at the top level.

## Resolve entry values

Creating an entry requires customer, project, category, start time, end time, and description. Date is optional and defaults to today.

- Accept dates as `YYYY-MM-DD` or `dd/MM/yyyy`.
- Use 24-hour `HH:mm` times.
- Customer, project, and category accept a numeric ID or a unique case-insensitive name substring.
- Run `<timesheet> meta --json` when the user has not supplied enough information or when a name is ambiguous.
- Preserve the meaning and factual details supplied by the user, but rewrite the description according to the Brazilian Portuguese policy below. Quote shell arguments safely.
- Use `--not-monetize` only when the user explicitly says the entry is non-monetized.

Do not guess among multiple customers, projects, or categories. Present the relevant choices and ask the user to select one.

## Write entry descriptions

Write every new or edited entry description in Brazilian Portuguese (`pt-BR`), even when the user's request is written in another language. Translate the supplied activity faithfully without inventing work, outcomes, technologies, or context.

Use an objective, professional, impersonal construction. Prefer action nouns such as “Implementação”, “Correção”, “Ajuste”, “Análise”, “Configuração”, “Validação” and “Revisão”. Avoid first-person language such as “eu fiz”, “trabalhei” or “nós implementamos”.

Good examples:

```text
Implementação de funcionalidade X.

Correção do fluxo de autenticação e validação do redirecionamento de sessão.

Análise do comportamento da integração com o serviço de pagamentos. Ajuste do tratamento de respostas inválidas.
```

Prefer one or more short prose paragraphs over bullets, numbered lists, checklists, fragments, or a sequence of hyphenated items. Use a single paragraph for a simple activity. When several related activities need separation, group complete sentences into short paragraphs.

## Read workflow

Read operations do not modify the user's timesheet and may be run immediately when requested.

Examples:

```sh
<timesheet> list --limit 10 --json
<timesheet> meta --json
<timesheet> status 12345 --json
<timesheet> invoice list --status pending --json
<timesheet> invoice preview 901 --json
<timesheet> upgrade --json
```

Summarize the result in the format the user requested. Preserve entry IDs because they are needed for status checks, updates, and deletion.

Invoice operations are read-only. Preserve process IDs because they connect the list and preview commands. The list returns normalized `pending` or `sent` status values and supports `--limit`, `--offset`, `--search`, `--status`, and `--all`. The preview returns the customer/project breakdown and monetized/non-monetized hours and amounts for one process. This CLI intentionally does not upload, open, or download invoice files.

## CLI upgrade workflow

Run `<timesheet> upgrade --json` only when the user explicitly asks to upgrade the installed CLI. `upgrade` retrieves the latest GitHub release, verifies the platform archive against `checksums.txt`, and replaces the current executable. Keep `<timesheet> update ID ...` exclusively for editing a timesheet entry.

For a private repository, use an already configured `GH_TOKEN` or `GITHUB_TOKEN`; never ask the user to paste a token into the conversation. If the executable directory is not writable, report the exact error and recommend rerunning the installer with a writable `INSTALL_DIR` or using the required operating-system permissions. Read `data.updated`, `data.currentVersion`, `data.latestVersion`, and `data.executable` from the JSON response. An `updated: false` result means the latest release was already installed and is successful, not an error.

## Create workflow

Before creating an entry, make sure every required value is known. Then run:

```sh
<timesheet> add \
  --customer CUSTOMER \
  --project PROJECT \
  --category CATEGORY \
  --date YYYY-MM-DD \
  --start HH:mm \
  --end HH:mm \
  --desc "Implementação de funcionalidade X." \
  --json
```

Omit `--date` only when the user clearly intends today. Add `--not-monetize` only when requested.

Creating an entry changes external data. The user's explicit request to create or log the entry is sufficient authorization; otherwise show the proposed values and ask for confirmation before running the command.

After success, read the created entry from `data.entry` and report its ID, recorded date/time, and final Brazilian Portuguese description. If the CLI rejects an ambiguous name, use `meta --json` to resolve it instead of repeatedly guessing.

## Update workflow

Find the entry ID with `list --json` if necessary. Pass only the fields the user wants changed; omitted fields retain their existing values.

```sh
<timesheet> update ID --end 18:00 --desc "Atualização da descrição da atividade." --json
```

An explicit request to edit a specific entry authorizes the update. If the requested change could apply to multiple entries, identify the candidates and ask the user which entry ID to change.

Whenever an update includes `--desc`, apply the Brazilian Portuguese, impersonal, paragraph-based writing policy before sending the value to the CLI.

Use `--not-monetize` to mark an entry non-monetized or `--monetize` to switch it back to monetized. The flags are mutually exclusive; omit both to preserve the current value.

## Delete workflow

Deletion is destructive. Resolve and show the exact entry ID, date, time, project, and category before deleting. Run:

```sh
<timesheet> delete ID --json
```

When an interactive prompt is available, let the CLI request confirmation. Use `--yes` only after the user has explicitly confirmed deletion of that exact entry and interaction is otherwise impossible. Never infer deletion approval from a general cleanup request when more than one entry could match. Read `data.deleted` to verify whether deletion occurred.

After success, report the deleted entry ID.

## Handle failures

- For `auth_required`, automatic renewal was unavailable or a write was deliberately not retried. Follow the exact message: ask for interactive login when needed, or rerun a write only after telling the user that the session was renewed and confirming the original operation did not succeed.
- For `login_failed` after automatic renewal, ask the user to run `<timesheet> login --save-credentials` interactively; rejected saved credentials are removed to avoid repeated attempts.
- Treat `credential_store_unavailable` and `session_persist_failed` warnings as nonfatal. Explain that the completed operation succeeded, but future automatic authentication may require login.
- For an unknown or ambiguous customer, project, or category, run `<timesheet> meta --json` and resolve it with the user.
- For a missing entry, refresh with `<timesheet> list --all --json` before concluding that it does not exist.
- For invalid dates or times, show the accepted format and ask for a corrected value.
- Do not retry write or delete commands blindly after an uncertain network failure. Read the entries first to determine whether the operation already succeeded.
- Return the exact CLI error when it helps the user act, but redact credentials, cookies, and sensitive session data.

## Help

Top-level and command-specific help are available with:

```sh
<timesheet> --help
<timesheet> -h
<timesheet> help
<timesheet> add --help
```
