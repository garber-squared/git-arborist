// Converts imports from '@testing-library/react-hooks' => '@testing-library/react'
const fs = require('fs');
const path = require('path');

const exts = new Set(['.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs']);
const roots = ['tests', 'test', '__tests__', 'src']; // adjust if needed

function* walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) yield* walk(full);
    else if (exts.has(path.extname(entry.name))) yield full;
  }
}

function rewriteImports(file) {
  const src = fs.readFileSync(file, 'utf8');
  if (!src.includes("@testing-library/react-hooks")) return false;
  const out = src.replace(/from\s+['"]@testing-library\/react-hooks['"]/g, "from '@testing-library/react'");
  if (out !== src) fs.writeFileSync(file, out, 'utf8');
  return out !== src;
}

let changed = 0;
for (const root of roots) {
  if (!fs.existsSync(root)) continue;
  for (const file of walk(root)) if (rewriteImports(file)) changed++;
}
console.log(`migrate-testing-lib: rewrote ${changed} file(s).`);
