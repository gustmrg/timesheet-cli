// HTML parsing helpers for the server-rendered pages.
// The app is ASP.NET MVC with fixed templates, so targeted regexes are
// sufficient and keep the CLI dependency-free.

export function decodeEntities(s) {
  return s
    .replace(/&#(\d+);/g, (_, n) => String.fromCodePoint(Number(n)))
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&nbsp;/g, ' ');
}

const stripTags = (s) => decodeEntities(s.replace(/<[^>]*>/g, '')).replace(/\s+/g, ' ').trim();

// Parse the entries table (#tbWorksheet) from GET /Worksheet/Read.
// Columns: Cliente | Projeto | Categoria | Data | Início | Fim | Total | Avaliação | Ações
export function parseEntries(html) {
  const tbody = html.match(/<tbody>([\s\S]*?)<\/tbody>/);
  if (!tbody) return [];
  const rows = tbody[1].match(/<tr[^>]*>[\s\S]*?<\/tr>/g) ?? [];
  return rows.map((row) => {
    const cells = [...row.matchAll(/<td[^>]*>([\s\S]*?)<\/td>/g)].map((c) => c[1]);
    const text = cells.map(stripTags);
    const evaluateAnchor = cells[7]?.match(/id="(\d+)"[^>]*name="Worksheet\|(\d+)"[^>]*title="([^"]*)"/);
    const editAnchor = row.match(/class="[^"]*editWorksheet[^"]*"[^>]*>/);
    const anyAnchor = cells[8]?.match(/id="(\d+)"/);
    return {
      id: Number(evaluateAnchor?.[1] ?? anyAnchor?.[1]),
      evaluateId: evaluateAnchor?.[2] ? Number(evaluateAnchor[2]) : null,
      customer: text[0],
      project: text[1],
      category: text[2],
      date: text[3], // dd/MM/yyyy
      start: text[4],
      end: text[5],
      total: text[6],
      status: evaluateAnchor ? evaluateAnchor[3] : text[7] || null,
    };
  });
}

// Parse <select class="form-control {cls}" name="{name}"> option lists.
export function parseSelects(html) {
  const selects = {};
  for (const m of html.matchAll(/<select class="form-control (\w+)" name="([^"]+)">([\s\S]*?)<\/select>/g)) {
    const [, cls, name, body] = m;
    selects[name] = [...body.matchAll(/<option([^>]*)>([\s\S]*?)<\/option>/g)]
      .map((o) => {
        const value = o[1].match(/value="([^"]*)"/)?.[1] ?? '';
        return { value, label: stripTags(o[2]) };
      })
      .filter((o) => o.value !== '');
    selects[cls] = selects[name]; // alias: customer/project/category
  }
  return selects;
}

// Hidden anti-forgery token from the login form.
export function parseVerificationToken(html) {
  return html.match(/name="__RequestVerificationToken"[^>]*value="([^"]+)"/)?.[1] ?? null;
}

// True when the "page" is actually the login screen (session expired).
export function isLoginPage(html, finalUrl) {
  if (/name="Password"/.test(html) && /name="Login"/.test(html)) return true;
  return false;
}
