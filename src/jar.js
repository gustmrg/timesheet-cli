// Minimal cookie jar for a single-site session.
// Persists as JSON: { "cookie-name": "value", ... }

import fs from 'node:fs';

export class Jar {
  constructor(file) {
    this.file = file;
    this.cookies = {};
  }

  static load(file) {
    const jar = new Jar(file);
    try {
      jar.cookies = JSON.parse(fs.readFileSync(file, 'utf8'));
    } catch {
      // no session yet — start empty
    }
    return jar;
  }

  save() {
    fs.mkdirSync(new URL('.', 'file://' + this.file + '/').pathname, { recursive: true });
    fs.writeFileSync(this.file, JSON.stringify(this.cookies, null, 2));
  }

  header() {
    return Object.entries(this.cookies)
      .map(([k, v]) => `${k}=${v}`)
      .join('; ');
  }

  get(name) {
    return this.cookies[name];
  }

  // Absorb Set-Cookie headers from a fetch Response.
  absorb(res) {
    for (const sc of res.headers.getSetCookie?.() ?? []) {
      const [pair] = sc.split(';');
      const eq = pair.indexOf('=');
      if (eq < 0) continue;
      const name = pair.slice(0, eq).trim();
      const value = pair.slice(eq + 1).trim();
      // honour deletions (empty value / Expires in the past)
      if (value === '' || /expires=.*1970/i.test(sc)) delete this.cookies[name];
      else this.cookies[name] = value;
    }
  }
}
