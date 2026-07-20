// Minimal cookie jar for a single-site session.
// Persists as JSON: { "cookie-name": "value", ... }

import fs from 'node:fs';
import path from 'node:path';

export class Jar {
  file: string;
  cookies: Record<string, string>;

  constructor(file: string) {
    this.file = file;
    this.cookies = {};
  }

  static load(file: string): Jar {
    const jar = new Jar(file);
    try {
      jar.cookies = JSON.parse(fs.readFileSync(file, 'utf8')) as Record<string, string>;
    } catch {
      // no session yet — start empty
    }
    return jar;
  }

  save(): void {
    fs.mkdirSync(path.dirname(this.file), { recursive: true });
    fs.writeFileSync(this.file, JSON.stringify(this.cookies, null, 2), { mode: 0o600 });
    fs.chmodSync(this.file, 0o600);
  }

  header(): string {
    return Object.entries(this.cookies)
      .map(([k, v]) => `${k}=${v}`)
      .join('; ');
  }

  get(name: string): string | undefined {
    return this.cookies[name];
  }

  // Absorb Set-Cookie headers from a fetch Response.
  absorb(res: Response): void {
    for (const sc of res.headers.getSetCookie()) {
      const [pair] = sc.split(';');
      if (!pair) continue;
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
