#!/usr/bin/env node
// timesheet — CLI for luby-timesheet.azurewebsites.net
//
// Commands:
//   login                                authenticate (stores session cookies)
//   list [--all] [--limit N] [--json]    list entries (most recent first)
//   meta [--json]                        customers, projects, categories
//   status <id> [--json]                 evaluation status of an entry
//   add --customer C --project P --category K --date D --start S --end E --desc T
//   update <id> [same flags as add]      edit an entry
//   delete <id> [--yes]                  delete an entry (asks confirmation)
//
// Dates: YYYY-MM-DD or dd/MM/yyyy (default: today). Times: HH:mm.
// Customer/project/category accept an id or a case-insensitive name substring.

import { parseArgs } from 'node:util';
import path from 'node:path';
import readline from 'node:readline';
import { fileURLToPath } from 'node:url';
import { Api, AuthError, type EntryInput, type EntryRef, type EvaluateInfo } from './src/api.ts';

const ROOT = path.dirname(fileURLToPath(import.meta.url));
const SESSION_FILE = process.env.TIMESHEET_SESSION ?? path.join(ROOT, 'session.json');

const [command, ...rest] = process.argv.slice(2);

const api = new Api(SESSION_FILE);

interface EntryFlags {
  customer?: string | undefined;
  project?: string | undefined;
  category?: string | undefined;
  date?: string | undefined;
  start?: string | undefined;
  end?: string | undefined;
  desc?: string | undefined;
  'not-monetize'?: boolean | undefined;
}

interface MaybeRef {
  id: string | number;
  name: string | null;
}

interface EntryBase {
  customer?: MaybeRef;
  project?: MaybeRef;
  category?: MaybeRef;
  date?: string;
  start?: string;
  end?: string;
  description?: string;
  notMonetize?: boolean;
}

async function main(): Promise<void> {
  switch (command) {
    case 'login': return cmdLogin(rest);
    case 'list': return cmdList(rest);
    case 'meta': return cmdMeta(rest);
    case 'status': return cmdStatus(rest);
    case 'add': return cmdAdd(rest);
    case 'update': return cmdUpdate(rest);
    case 'delete': return cmdDelete(rest);
    case undefined:
    case 'help':
    case '--help':
      console.log(USAGE);
      return;
    default:
      console.error(`unknown command: ${command}\n`);
      console.log(USAGE);
      process.exit(1);
  }
}

const USAGE = `usage: timesheet <command>

  login                          authenticate with your site credentials
  list [--all] [--limit N]       list entries (default: 20 most recent)
  meta                           list customers, projects and categories
  status <id>                    evaluation status of entry <id>
  add    --customer C --project P --category K
         [--date D] --start S --end E --desc "text" [--not-monetize]
  update <id> [any add flags]    edit entry <id> (unspecified fields kept)
  delete <id> [--yes]            delete entry <id>

  customer/project/category: numeric id or name substring
  date: YYYY-MM-DD or dd/MM/yyyy (default: today); times: HH:mm
  global: --json for machine-readable output
`;

// --- commands -------------------------------------------------------------

async function cmdLogin(argv: string[]): Promise<void> {
  const { values } = parseArgs({
    args: argv,
    options: {
      user: { type: 'string' },
      pass: { type: 'string' },
    },
  });
  const user = values.user ?? process.env.TIMESHEET_USER ?? (await ask('Login: '));
  const pass = values.pass ?? process.env.TIMESHEET_PASS ?? (await askHidden('Password: '));
  await api.login(user, pass);
  console.log('logged in — session saved to', SESSION_FILE);
}

async function cmdList(argv: string[]): Promise<void> {
  const { values } = parseArgs({
    args: argv,
    options: {
      all: { type: 'boolean' },
      limit: { type: 'string' },
      json: { type: 'boolean' },
    },
  });
  let entries = await api.listEntries();
  const limit = values.all ? Infinity : Number(values.limit ?? 20);
  const total = entries.length;
  entries = entries.slice(0, limit);
  if (values.json) return printJson(entries);

  const rows = entries.map((e) => [
    String(e.id),
    e.date,
    `${e.start}–${e.end}`,
    e.total,
    e.status ?? '',
    e.project,
    e.category,
  ]);
  printTable(['ID', 'DATE', 'TIME', 'TOTAL', 'STATUS', 'PROJECT', 'CATEGORY'], rows);
  if (total > entries.length) console.log(`… ${total - entries.length} more (use --all)`);
}

interface MetaCustomer {
  id: string;
  name: string;
  projects: Array<{ id: number; name: string; categories: Array<{ id: number; name: string }> }>;
}

async function cmdMeta(argv: string[]): Promise<void> {
  const { values } = parseArgs({ args: argv, options: { json: { type: 'boolean' } } });
  const meta = await api.getMeta();
  const out: MetaCustomer[] = [];
  for (const c of meta.customer ?? []) {
    const projects = await api.dropDownChange({ idcustomer: c.value });
    const projList: MetaCustomer['projects'] = [];
    for (const p of projects) {
      const categories = await api.dropDownChange({ idproject: p.IdProject });
      projList.push({
        id: p.IdProject,
        name: p.ProjectName,
        categories: categories.map((k) => ({ id: k.IdCategory, name: k.CategoryName })),
      });
    }
    out.push({ id: c.value, name: c.label, projects: projList });
  }
  if (values.json) return printJson(out);
  for (const c of out) {
    console.log(`${c.id}  ${c.name}`);
    for (const p of c.projects) {
      console.log(`  ${p.id}  ${p.name}`);
      for (const k of p.categories) console.log(`    ${k.id}  ${k.name}`);
    }
  }
}

async function cmdStatus(argv: string[]): Promise<void> {
  const { values, positionals } = parseArgs({
    args: argv,
    allowPositionals: true,
    options: { json: { type: 'boolean' } },
  });
  const id = positionals[0];
  if (!id) die('usage: timesheet status <entry-id>');

  const entry = (await api.listEntries()).find((e) => e.id === Number(id));
  if (!entry) die(`entry ${id} not found`);
  if (!entry.evaluateId) die(`entry ${id} has no evaluation`);

  const ev: EvaluateInfo = await api.readEvaluate(entry.id, entry.evaluateId);
  const state =
    ev.IsApprove === '1' ? 'Aprovado'
    : ev.IsReprove === '1' ? 'Reprovado'
    : ev.IsPreApproved === '1' ? 'Pré-aprovado'
    : ev.IsPreReproved === '1' ? 'Pré-reprovado'
    : ev.IsReview === '1' ? 'Em revisão'
    : 'Em análise';
  if (values.json) return printJson(ev);
  console.log(`entry ${entry.id} (${entry.date} ${entry.start}–${entry.end}, ${entry.project})`);
  console.log(`status:  ${state}`);
  console.log(`manager: ${ev.ManagerName ?? '—'}`);
  console.log(`created: ${ev.Created ?? '—'}`);
}

async function cmdAdd(argv: string[]): Promise<void> {
  const { values } = parseArgs({ args: argv, options: addOptions() });
  const entry = await resolveEntry(values, {});
  const res = await api.createEntry(entry);
  if (!res.success) die(res.message ?? 'create failed');
  const created = res.createdWorksheets?.[0];
  console.log(`created entry ${created?.Id ?? '?'} (${entry.date} ${entry.start}–${entry.end}, ${entry.project.name})`);
}

async function cmdUpdate(argv: string[]): Promise<void> {
  const { values, positionals } = parseArgs({
    args: argv,
    allowPositionals: true,
    options: addOptions(),
  });
  const id = positionals[0];
  if (!id) die('usage: timesheet update <entry-id> [flags]');

  const cur = await api.getWorksheet(id);
  if (!cur?.Id) die(`entry ${id} not found`);
  const base: EntryBase = {
    customer: { id: cur.IdCustomer, name: null },
    project: { id: cur.IdProject, name: null },
    category: { id: cur.IdCategory, name: null },
    date: cur.InformedDate,
    start: cur.StartTime,
    end: cur.EndTime,
    description: cur.Description,
    notMonetize: cur.NotMonetize,
  };
  const entry = await resolveEntry(values, base);
  const res = await api.updateEntry(id, entry);
  if (!res.success) die(res.message ?? 'update failed');
  console.log(`updated entry ${id} (${entry.date} ${entry.start}–${entry.end}, ${entry.project.name})`);
}

async function cmdDelete(argv: string[]): Promise<void> {
  const { values, positionals } = parseArgs({
    args: argv,
    allowPositionals: true,
    options: { yes: { type: 'boolean' }, json: { type: 'boolean' } },
  });
  const id = positionals[0];
  if (!id) die('usage: timesheet delete <entry-id> [--yes]');

  if (!values.yes) {
    const entry = (await api.listEntries()).find((e) => e.id === Number(id));
    const what = entry ? `${entry.date} ${entry.start}–${entry.end} ${entry.project} (${entry.category})` : `id ${id}`;
    const ok = await ask(`Delete entry ${what}? [y/N] `);
    if (!/^y(es)?$/i.test(ok.trim())) return console.log('aborted');
  }
  const res = await api.deleteEntry(id);
  if (!res.success) die(res.message ?? 'delete failed');
  console.log(`deleted entry ${id}`);
}

// --- helpers ---------------------------------------------------------------

function addOptions() {
  return {
    customer: { type: 'string' },
    project: { type: 'string' },
    category: { type: 'string' },
    date: { type: 'string' },
    start: { type: 'string' },
    end: { type: 'string' },
    desc: { type: 'string' },
    'not-monetize': { type: 'boolean' },
  } as const;
}

// Merge CLI flags over a base entry, resolving ids/names via site metadata.
async function resolveEntry(values: EntryFlags, base: EntryBase): Promise<EntryInput> {
  const meta = await api.getMeta();
  const customers: EntryRef[] = (meta.customer ?? []).map((c) => ({ id: c.value, name: c.label }));
  const customer = await pick('customer', values.customer ?? base.customer?.id, customers, base.customer);

  const projects: EntryRef[] = (await api.dropDownChange({ idcustomer: customer.id })).map((p) => ({
    id: p.IdProject,
    name: p.ProjectName,
  }));
  const project = await pick('project', values.project ?? base.project?.id, projects, base.project);

  const categories: EntryRef[] = (await api.dropDownChange({ idproject: project.id })).map((k) => ({
    id: k.IdCategory,
    name: k.CategoryName,
  }));
  const category = await pick('category', values.category ?? base.category?.id, categories, base.category);

  const entry: EntryInput = {
    customer,
    project,
    category,
    date: toWireDate(values.date ?? base.date ?? isoToday()),
    start: values.start ?? base.start ?? '',
    end: values.end ?? base.end ?? '',
    description: values.desc ?? base.description ?? '',
    notMonetize: values['not-monetize'] ?? base.notMonetize ?? false,
  };
  if (!entry.start) die('missing --start');
  if (!entry.end) die('missing --end');
  if (!entry.description) die('missing --desc');
  return entry;
}

// Match a flag value against options: numeric id or case-insensitive substring.
async function pick(
  kind: string,
  wanted: string | number | undefined,
  options: EntryRef[],
  fallback?: MaybeRef,
): Promise<EntryRef> {
  if (wanted == null) die(`missing --${kind}`);
  const w = String(wanted).toLowerCase();
  const byId = options.find((o) => String(o.id) === String(wanted));
  if (byId) return byId;
  const matches = options.filter((o) => o.name.toLowerCase().includes(w));
  if (matches.length === 1) return matches[0]!;
  if (matches.length > 1) die(`--${kind} "${wanted}" is ambiguous: ${matches.map((m) => m.name).join(', ')}`);
  if (fallback && String(fallback.id) === String(wanted) && fallback.name != null) {
    return { id: fallback.id, name: fallback.name };
  }
  die(`unknown ${kind}: "${wanted}" (see \`timesheet meta\`)`);
}

function toWireDate(d: string): string {
  let m = d.match(/^(\d{4})-(\d{2})-(\d{2})$/); // ISO
  if (m) return `${m[3]}/${m[2]}/${m[1]}`;
  m = d.match(/^(\d{2})\/(\d{2})\/(\d{4})$/); // already wire format
  if (m) return d;
  die(`invalid date "${d}" — use YYYY-MM-DD or dd/MM/yyyy`);
}

const isoToday = (): string => new Date().toLocaleDateString('sv-SE'); // YYYY-MM-DD

function printTable(header: string[], rows: string[][]): void {
  const widths = header.map((h, i) => Math.max(h.length, ...rows.map((r) => String(r[i]).length)));
  const line = (cols: string[]): string =>
    cols.map((c, i) => String(c).padEnd(widths[i] ?? 0)).join('  ').trimEnd();
  console.log(line(header));
  console.log(widths.map((w) => '─'.repeat(w)).join('  '));
  for (const r of rows) console.log(line(r));
}

const printJson = (obj: unknown): void => console.log(JSON.stringify(obj, null, 2));

function die(msg: string): never {
  console.error(`error: ${msg}`);
  process.exit(1);
}

function ask(question: string): Promise<string> {
  const rl = readline.createInterface({ input: process.stdin, output: process.stdout });
  return new Promise((resolve) => rl.question(question, (a) => { rl.close(); resolve(a); }));
}

function askHidden(question: string): Promise<string> {
  if (!process.stdin.isTTY) die('no TTY — pass --pass or TIMESHEET_PASS');
  return new Promise((resolve) => {
    process.stdout.write(question);
    const stdin = process.stdin;
    let buf = '';
    const cleanup = (): void => {
      stdin.setRawMode(false);
      stdin.pause();
      stdin.off('data', onData);
    };
    const onData = (ch: string): void => {
      if (ch === '\n' || ch === '\r') { cleanup(); process.stdout.write('\n'); resolve(buf); }
      else if (ch === '') { cleanup(); process.exit(130); }
      else if (ch === '') buf = buf.slice(0, -1);
      else buf += ch;
    };
    stdin.setRawMode(true);
    stdin.resume();
    stdin.setEncoding('utf8');
    stdin.on('data', onData);
  });
}

main().catch((err: unknown) => {
  console.error(`error: ${err instanceof Error ? err.message : String(err)}`);
  process.exit(1);
});
