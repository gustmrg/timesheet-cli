// Thin API client for luby-timesheet (ASP.NET MVC, cookie auth).
// Endpoint map derived from a recorded browser session (capture/).

import { Jar } from './jar.js';
import { parseEntries, parseSelects, parseVerificationToken, isLoginPage } from './parse.js';

const BASE = process.env.TIMESHEET_BASE_URL ?? 'https://luby-timesheet.azurewebsites.net';

export class AuthError extends Error {
  constructor() {
    super('Session expired or missing. Run `timesheet login` first.');
    this.name = 'AuthError';
  }
}

export class Api {
  constructor(sessionFile) {
    this.jar = Jar.load(sessionFile);
  }

  saveSession() {
    this.jar.save();
  }

  // fetch with cookie jar + manual redirect handling (so we capture
  // Set-Cookie on every hop, which fetch's redirect:"follow" hides).
  async request(path, { method = 'GET', query, form, json, xhr = false } = {}) {
    let url = path.startsWith('http') ? path : BASE + path;
    if (query) url += '?' + new URLSearchParams(query);

    const headers = { cookie: this.jar.header() };
    if (xhr) headers['x-requested-with'] = 'XMLHttpRequest';
    let body;
    if (form) {
      headers['content-type'] = 'application/x-www-form-urlencoded; charset=UTF-8';
      body = new URLSearchParams(form).toString();
    } else if (json) {
      headers['content-type'] = 'application/json';
      body = JSON.stringify(json);
    }

    for (let hop = 0; hop < 6; hop++) {
      const res = await fetch(url, { method, headers, body, redirect: 'manual' });
      this.jar.absorb(res);
      if ([301, 302, 303, 307, 308].includes(res.status)) {
        const loc = res.headers.get('location');
        if (!loc) return res;
        url = new URL(loc, url).href;
        if ([301, 302, 303].includes(res.status)) { method = 'GET'; body = undefined; }
        continue;
      }
      return res;
    }
    throw new Error('Too many redirects');
  }

  async text(path, opts) {
    const res = await this.request(path, opts);
    return { status: res.status, body: await res.text(), url: res.url };
  }

  async json(path, opts) {
    const res = await this.request(path, opts);
    const body = await res.text();
    try {
      return { status: res.status, data: JSON.parse(body) };
    } catch {
      if (isLoginPage(body, res.url)) throw new AuthError();
      throw new Error(`Expected JSON from ${path}, got: ${body.slice(0, 120)}`);
    }
  }

  // --- auth ---------------------------------------------------------------

  async login(user, password) {
    const landing = await this.text('/');
    const token = parseVerificationToken(landing.body);
    if (!token) throw new Error('Could not find __RequestVerificationToken on login page');

    const res = await this.request('/', {
      method: 'POST',
      form: { __RequestVerificationToken: token, Login: user, Password: password },
    });
    const html = await res.text();
    if (isLoginPage(html, res.url)) {
      // do NOT save — the jar now holds anonymous cookies that would
      // clobber the existing session on disk
      throw new Error('Login failed — check your credentials');
    }
    this.saveSession();
    return true;
  }

  // Throws AuthError when the session cookie is no longer accepted.
  async assertAuthenticated() {
    const { body } = await this.text('/Home/Index');
    if (isLoginPage(body)) throw new AuthError();
  }

  // --- reads --------------------------------------------------------------

  async readPage() {
    const { body } = await this.text('/Worksheet/Read');
    if (isLoginPage(body)) throw new AuthError();
    return body;
  }

  async listEntries() {
    return parseEntries(await this.readPage());
  }

  // Customers (+ the create/edit form selects) from the embedded dropdowns.
  async getMeta() {
    return parseSelects(await this.readPage());
  }

  // DropDownChange?idcustomer=N -> projects for a customer
  // DropDownChange?idproject=N  -> categories for a project
  async dropDownChange(params) {
    const { data } = await this.json('/Worksheet/DropDownChange', { query: params, xhr: true });
    return data;
  }

  async getWorksheet(id) {
    const { data } = await this.json('/Worksheet/Update', { query: { id }, xhr: true });
    return data;
  }

  async readEvaluate(idWorksheet, idEvaluate) {
    const { data } = await this.json('/Worksheet/ReadEvaluate', {
      method: 'POST',
      form: { idworksheet: idWorksheet, idevaluate: idEvaluate },
      xhr: true,
    });
    return data;
  }

  // --- writes -------------------------------------------------------------

  // entry: { customer:{id,name}, project:{id,name}, category:{id,name},
  //          date:'dd/MM/yyyy', start:'HH:mm', end:'HH:mm',
  //          description:'plain text', notMonetize:bool }
  async createEntry(entry) {
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
    const { data } = await this.json('/Worksheet/UpdateMultiple', {
      method: 'POST',
      json: { WorksheetMultiple: [item] },
      xhr: true,
    });
    return data; // { success, message, createdWorksheets: [...] }
  }

  async updateEntry(id, entry) {
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
    const { data } = await this.json('/Worksheet/UpdateOne', {
      method: 'POST',
      json: payload,
      xhr: true,
    });
    return data; // { success, message, createdWorksheet: {...} }
  }

  async deleteEntry(id) {
    const { data } = await this.json('/Worksheet/Delete', {
      method: 'POST',
      form: { id },
      xhr: true,
    });
    return data; // { success, message }
  }
}

// The app stores descriptions as HTML (<p>...</p>); wrap plain text.
function toHtml(text) {
  if (/^\s*<[a-z]/i.test(text)) return text; // already HTML
  return text
    .split(/\n{2,}/)
    .map((p) => `<p>${p.replace(/\n/g, '<br>')}</p>`)
    .join('');
}
