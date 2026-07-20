---
name: timesheet-cli
description: Manage Luby Timesheet entries with the local `timesheet` CLI. Use this skill whenever the user asks to log hours, create or edit a timesheet entry, list recent time records, inspect customers/projects/categories, check approval status, delete an entry, authenticate the timesheet CLI, or automate Luby Timesheet work—even when they do not explicitly mention this skill or the CLI.
compatibility: Requires Node.js 24+ and either the `timesheet` command on PATH or a local checkout of timesheet-cli.
---

# Luby Timesheet CLI

Use the `timesheet` CLI to manage the user's Luby Timesheet records. Prefer the CLI over direct HTTP requests because it owns authentication, input normalization, metadata resolution, and server-specific behavior.

## Locate the CLI

First check whether the linked command is available:

```sh
command -v timesheet
```

If it is available, use `timesheet`. When working inside the `timesheet-cli` repository and the linked command is unavailable, use:

```sh
node ./timesheet.ts
```

Refer to the selected invocation as `<timesheet>` in the workflow below. Do not install packages or create a global npm link unless the user asks for installation.

## Protect authentication

The CLI stores session cookies at `~/.timesheet-cli/session.json` by default. `TIMESHEET_SESSION` can override that location.

- Never read, print, copy, summarize, commit, or expose the session file. It contains active authentication cookies.
- Never ask the user to paste a password into the conversation.
- Do not put passwords in command arguments, environment variables, logs, or scripts on the user's behalf.
- If authentication is missing or expired, ask the user to run `<timesheet> login` in an interactive terminal. Continue after they confirm that login succeeded.
- Do not use the developer capture script for normal timesheet work.

## Choose the command

| User intent | Command |
| --- | --- |
| Authenticate | `<timesheet> login` |
| List recent entries | `<timesheet> list --json` |
| List all entries | `<timesheet> list --all --json` |
| Limit returned entries | `<timesheet> list --limit N --json` |
| Discover customers, projects, and categories | `<timesheet> meta --json` |
| Check an entry's evaluation | `<timesheet> status ID --json` |
| Create an entry | `<timesheet> add ...` |
| Change an entry | `<timesheet> update ID ...` |
| Delete an entry | `<timesheet> delete ID` |

Use `--json` for read commands because structured output is safer to interpret than terminal tables. The write commands print a concise human-readable result and do not support JSON output.

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
```

Summarize the result in the format the user requested. Preserve entry IDs because they are needed for status checks, updates, and deletion.

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
  --desc "Implementação de funcionalidade X."
```

Omit `--date` only when the user clearly intends today. Add `--not-monetize` only when requested.

Creating an entry changes external data. The user's explicit request to create or log the entry is sufficient authorization; otherwise show the proposed values and ask for confirmation before running the command.

After success, report the created entry ID, recorded date/time, and final Brazilian Portuguese description. If the CLI rejects an ambiguous name, use `meta --json` to resolve it instead of repeatedly guessing.

## Update workflow

Find the entry ID with `list --json` if necessary. Pass only the fields the user wants changed; omitted fields retain their existing values.

```sh
<timesheet> update ID --end 18:00 --desc "Atualização da descrição da atividade."
```

An explicit request to edit a specific entry authorizes the update. If the requested change could apply to multiple entries, identify the candidates and ask the user which entry ID to change.

Whenever an update includes `--desc`, apply the Brazilian Portuguese, impersonal, paragraph-based writing policy before sending the value to the CLI.

The current CLI can set `--not-monetize`, but it cannot explicitly switch that value back off. Explain this limitation instead of claiming a monetized update succeeded.

## Delete workflow

Deletion is destructive. Resolve and show the exact entry ID, date, time, project, and category before deleting. Run:

```sh
<timesheet> delete ID
```

When an interactive prompt is available, let the CLI request confirmation. Use `--yes` only after the user has explicitly confirmed deletion of that exact entry and interaction is otherwise impossible. Never infer deletion approval from a general cleanup request when more than one entry could match.

After success, report the deleted entry ID.

## Handle failures

- For `Session expired or missing`, pause and ask the user to run `<timesheet> login` interactively.
- For an unknown or ambiguous customer, project, or category, run `<timesheet> meta --json` and resolve it with the user.
- For a missing entry, refresh with `<timesheet> list --all --json` before concluding that it does not exist.
- For invalid dates or times, show the accepted format and ask for a corrected value.
- Do not retry write or delete commands blindly after an uncertain network failure. Read the entries first to determine whether the operation already succeeded.
- Return the exact CLI error when it helps the user act, but redact credentials, cookies, and sensitive session data.

## Help

Top-level help is available with:

```sh
<timesheet> --help
<timesheet> help
```

The CLI does not currently support `-h` or command-specific `--help` flags. Use this skill or the repository README for command details.
