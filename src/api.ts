// Thin API client for luby-timesheet (ASP.NET MVC, cookie auth).
// Endpoint map derived from a recorded browser session (capture/).

import { Jar } from './jar.ts';
import {
  parseEntries,
  parseSelects,
  parseVerificationToken,
  isLoginPage,
  type WorksheetEntry,
  type SelectOption,
} from './parse.ts';

const BASE = process.env.TIMESHEET_BASE_URL ?? 'https://luby-timesheet.azurewebsites.net';

export class AuthError extends Error {
  constructor() {
    super('Session expired or missing. Run `timesheet login` first.');
    this.name = 'AuthError';
  }
}

export interface EntryRef {
  id: string | number;
  name: string;
}

export interface EntryInput {
  customer: EntryRef;
  project: EntryRef;
  category: EntryRef;
  date: string; // dd/MM/yyyy (wire format)
  start: string; // HH:mm
  end: string; // HH:mm
  description: string; // plain text (wrapped in <p>) or raw HTML
  notMonetize: boolean;
}

interface RequestOptions {
  method?: string;
  query?: Record<string, string | number>;
  form?: Record<string, string | number>;
  json?: unknown;
  xhr?: boolean;
}

export interface WriteResult {
  success: boolean;
  message?: string;
  createdWorksheets?: Array<{ Id: number }>;
  createdWorksheet?: { Id: number };
}

export interface EvaluateInfo {
  ManagerName?: string | null;
  Created?: string | null;
  IsWait?: string;
  IsApprove?: string;
  IsReprove?: string;
  IsPreApproved?: string;
  IsPreReproved?: string;
  IsReview?: string;
  [key: string]: unknown;
}

export interface WorksheetRecord {
  Id: number;
  IdCustomer: number;
  IdProject: number;
  IdCategory: number;
  InformedDate: string;
  StartTime: string;
  EndTime: string;
  Description: string;
  NotMonetize: boolean;
  [key: string]: unknown;
}

export interface DropDownRecord {
  IdCustomer: number;
  CustomerName: string;
  IdProject: number;
  ProjectName: string;
  IdCategory: number;
  CategoryName: string;
  IdDeveloper: number;
  IsDeleted: boolean;
}

export class Api {
  jar: Jar;

  constructor(sessionFile: string) {
    this.jar = Jar.load(sessionFile);
  }

  saveSession(): void {
    this.jar.save();
  }

  // fetch with cookie jar + manual redirect handling (so we capture
  // Set-Cookie on every hop, which fetch's redirect:"follow" hides).
  async request(path: string, { method = 'GET', query, form, json, xhr = false }: RequestOptions = {}): Promise<Response> {
    let url = path.startsWith('http') ? path : BASE + path;
    if (query) url += '?' + new URLSearchParams(stringify(query));

    const headers: Record<string, string> = { cookie: this.jar.header() };
    if (xhr) headers['x-requested-with'] = 'XMLHttpRequest';
    let body: string | undefined;
    if (form) {
      headers['content-type'] = 'application/x-www-form-urlencoded; charset=UTF-8';
      body = new URLSearchParams(stringify(form)).toString();
    } else if (json !== undefined) {
      headers['content-type'] = 'application/json';
      body = JSON.stringify(json);
    }

    let currentMethod = method;
    for (let hop = 0; hop < 6; hop++) {
      const res = await fetch(url, { method: currentMethod, headers, body: body ?? null, redirect: 'manual' });
      this.jar.absorb(res);
      if ([301, 302, 303, 307, 308].includes(res.status)) {
        const loc = res.headers.get('location');
        if (!loc) return res;
        url = new URL(loc, url).href;
        if ([301, 302, 303].includes(res.status)) {
          currentMethod = 'GET';
          body = undefined;
        }
        continue;
      }
      return res;
    }
    throw new Error('Too many redirects');
  }

  async text(path: string, opts?: RequestOptions): Promise<{ status: number; body: string }> {
    const res = await this.request(path, opts);
    return { status: res.status, body: await res.text() };
  }

  async json<T>(path: string, opts?: RequestOptions): Promise<{ status: number; data: T }> {
    const res = await this.request(path, opts);
    const body = await res.text();
    if (isLoginPage(body)) throw new AuthError();
    try {
      return { status: res.status, data: JSON.parse(body) as T };
    } catch {
      throw new Error(`Expected JSON from ${path}, got: ${body.slice(0, 120)}`);
    }
  }

  // --- auth ---------------------------------------------------------------

  async login(user: string, password: string): Promise<true> {
    const landing = await this.text('/');
    const token = parseVerificationToken(landing.body);
    if (!token) throw new Error('Could not find __RequestVerificationToken on login page');

    const res = await this.request('/', {
      method: 'POST',
      form: { __RequestVerificationToken: token, Login: user, Password: password },
    });
    const html = await res.text();
    if (isLoginPage(html)) {
      // do NOT save — the jar now holds anonymous cookies that would
      // clobber the existing session on disk
      throw new Error('Login failed — check your credentials');
    }
    this.saveSession();
    return true;
  }

  // --- reads --------------------------------------------------------------

  async readPage(): Promise<string> {
    const { body } = await this.text('/Worksheet/Read');
    if (isLoginPage(body)) throw new AuthError();
    return body;
  }

  async listEntries(): Promise<WorksheetEntry[]> {
    return parseEntries(await this.readPage());
  }

  // Customers (+ the create/edit form selects) from the embedded dropdowns.
  async getMeta(): Promise<Record<string, SelectOption[]>> {
    return parseSelects(await this.readPage());
  }

  // DropDownChange?idcustomer=N -> projects for a customer
  // DropDownChange?idproject=N  -> categories for a project
  async dropDownChange(params: { idcustomer?: string | number; idproject?: string | number }): Promise<DropDownRecord[]> {
    const { data } = await this.json<DropDownRecord[]>('/Worksheet/DropDownChange', { query: params, xhr: true });
    return data;
  }

  async getWorksheet(id: string | number): Promise<WorksheetRecord> {
    const { data } = await this.json<WorksheetRecord>('/Worksheet/Update', { query: { id }, xhr: true });
    return data;
  }

  async readEvaluate(idWorksheet: string | number, idEvaluate: string | number): Promise<EvaluateInfo> {
    const { data } = await this.json<EvaluateInfo>('/Worksheet/ReadEvaluate', {
      method: 'POST',
      form: { idworksheet: idWorksheet, idevaluate: idEvaluate },
      xhr: true,
    });
    return data;
  }

  // --- writes -------------------------------------------------------------

  async createEntry(entry: EntryInput): Promise<WriteResult> {
    const item = {
      IdCustomer: String(entry.customer.id),
      CustomerName: entry.customer.name,
      IdProject: String(entry.project.id),
      ProjectName: entry.project.name,
      IdCategory: String(entry.category.id),
      CategoryName: entry.category.name,
      InformedDate: entry.date,
      StartTime: entry.start,
      EndTime: entry.end,
      Description: toHtml(entry.description),
      NotMonetize: !!entry.notMonetize,
    };
    const { data } = await this.json<WriteResult>('/Worksheet/UpdateMultiple', {
      method: 'POST',
      json: { WorksheetMultiple: [item] },
      xhr: true,
    });
    return data;
  }

  async updateEntry(id: string | number, entry: EntryInput): Promise<WriteResult> {
    const payload = {
      id: String(id),
      idcustomer: String(entry.customer.id),
      customername: entry.customer.name,
      idproject: String(entry.project.id),
      projectname: entry.project.name,
      idcategory: String(entry.category.id),
      categoryname: entry.category.name,
      informeddate: entry.date,
      starttime: entry.start,
      endtime: entry.end,
      description: toHtml(entry.description),
      notmonetize: !!entry.notMonetize,
    };
    const { data } = await this.json<WriteResult>('/Worksheet/UpdateOne', {
      method: 'POST',
      json: payload,
      xhr: true,
    });
    return data;
  }

  async deleteEntry(id: string | number): Promise<WriteResult> {
    const { data } = await this.json<WriteResult>('/Worksheet/Delete', {
      method: 'POST',
      form: { id },
      xhr: true,
    });
    return data;
  }
}

function stringify(params: Record<string, string | number>): Record<string, string> {
  return Object.fromEntries(Object.entries(params).map(([k, v]) => [k, String(v)]));
}

// The app stores descriptions as HTML (<p>...</p>); wrap plain text.
function toHtml(text: string): string {
  if (/^\s*<[a-z]/i.test(text)) return text; // already HTML
  return text
    .split(/\n{2,}/)
    .map((p) => `<p>${p.replace(/\n/g, '<br>')}</p>`)
    .join('');
}
