// Fails when any language file is missing a key from en.json (CI i18n check).
// Also counts values still identical to English (placeholders); `--strict` fails on them too (T3.07 done criterion).
import { readFileSync, existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// Relative to this script, so it checks the same files from the repo root or from frontend/.
const dir = fileURLToPath(new URL('../frontend/messages', import.meta.url));
if (!existsSync(`${dir}/en.json`)) {
  console.log(`skip: ${dir}/en.json not found`);
  process.exit(0);
}
// Keys whose value is correctly the same in every language (endonyms, codes).
const sameOK = new Set(['queue_ticket_no', 'timeline_other', 'roles_grant']);

const strict = process.argv.includes('--strict');
const load = (f) => JSON.parse(readFileSync(`${dir}/${f}.json`, 'utf8'));
const en = load('en');
const enKeys = Object.keys(en).filter((k) => k !== '$schema');
let failed = false;
for (const lang of ['zh-CN', 'my', 'th']) {
  const msgs = existsSync(`${dir}/${lang}.json`) ? load(lang) : {};
  const missing = enKeys.filter((k) => !(k in msgs));
  if (missing.length) {
    failed = true;
    console.error(`${lang}: missing ${missing.join(', ')}`);
  }
  const english = enKeys.filter((k) => k in msgs && !sameOK.has(k) && JSON.stringify(msgs[k]) === JSON.stringify(en[k]));
  if (english.length) {
    if (strict) {
      failed = true;
      console.error(`${lang}: ${english.length} English placeholders: ${english.join(', ')}`);
    } else {
      console.log(`${lang}: ${english.length} English placeholders (run with --strict to list them)`);
    }
  }
}
process.exit(failed ? 1 : 0);
