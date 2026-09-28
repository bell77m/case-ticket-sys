// Fails when any language file is missing a key from en.json (CI i18n check).
import { readFileSync, existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// Relative to this script, so it checks the same files from the repo root or from frontend/.
const dir = fileURLToPath(new URL('../frontend/messages', import.meta.url));
if (!existsSync(`${dir}/en.json`)) {
  console.log(`skip: ${dir}/en.json not found`);
  process.exit(0);
}
const keys = (f) => Object.keys(JSON.parse(readFileSync(`${dir}/${f}.json`, 'utf8'))).filter((k) => k !== '$schema');
const en = keys('en');
let failed = false;
for (const lang of ['zh-CN', 'my', 'th']) {
  const have = new Set(existsSync(`${dir}/${lang}.json`) ? keys(lang) : []);
  const missing = en.filter((k) => !have.has(k));
  if (missing.length) {
    failed = true;
    console.error(`${lang}: missing ${missing.join(', ')}`);
  }
}
process.exit(failed ? 1 : 0);
