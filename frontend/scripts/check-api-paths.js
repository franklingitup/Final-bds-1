#!/usr/bin/env node
// check-api-paths.js
// Static analysis: scan frontend/src/lib/api/*.ts for API paths that bypass
// the required /v1 prefix.  Mirrors the runtime guard in ApiClient.buildUrl().
//
// Run:  node scripts/check-api-paths.js
// CI:   npm run check:api-paths
'use strict';

const fs   = require('fs');
const path = require('path');

// ─── 1. Inline test of the guard predicate ────────────────────────────────────
// Reimplements the same condition used in client.ts buildUrl() so we can
// confirm the logic fires correctly without needing a full test framework.

function guardFires(p) { return !p.startsWith('/v1/'); }

let guardOk = true;

// Bad paths – must fire
const BAD = [
  '/organizations/foo',
  '/auth/login',
  '/providers/aws/regions',
];
for (const p of BAD) {
  if (!guardFires(p)) {
    console.error(`FAIL guard: expected "${p}" to trigger but it didn't`);
    guardOk = false;
  }
}

// Good paths – must NOT fire
const GOOD = [
  '/v1/organizations/abc/clusters',
  '/v1/auth/login',
  '/v1/invitations/accept',
  '/v1/organizations/abc/providers/aws/regions',
];
for (const p of GOOD) {
  if (guardFires(p)) {
    console.error(`FAIL guard: expected "${p}" to pass but it was rejected`);
    guardOk = false;
  }
}

if (guardOk) {
  console.log('[check-api-paths] Guard logic: OK');
} else {
  process.exit(1);
}

// ─── 2. Static scan ───────────────────────────────────────────────────────────
// Rules:
//   • Skip client.ts (the base client itself, buildUrl lives there)
//   • Skip index.ts  (re-export barrel, no path strings)
//   • For each remaining line, strip everything from // onward (handles
//     CRLF and inline comments uniformly) before testing patterns
//   • Fail on any code line that contains /organizations/ but NOT /v1/organizations/
//   • Fail on any apiClient call whose first path arg starts with "/" but not "/v1/"

const API_DIR = path.join(__dirname, '..', 'src', 'lib', 'api');
const SKIP    = new Set(['client.ts', 'index.ts']);

// Pattern A: /organizations/ without the /v1/ prefix
const RE_BARE_ORG = /\/organizations\//;
const RE_V1_ORG   = /\/v1\/organizations\//;

// Pattern B: apiClient call whose first string arg starts with "/" but not "/v1/"
// Handles both double-quoted and template-literal paths.
const RE_API_CALL  = /apiClient\s*\.\s*(?:get|post|patch|delete|request)\s*(?:<[^>]*>)?\s*\(\s*[`"'](\/.+)/;
const RE_GOOD_PATH = /^\/v1\//;

const files = fs.readdirSync(API_DIR)
  .filter(f => f.endsWith('.ts') && !SKIP.has(f))
  .sort();

let errors = 0;

for (const file of files) {
  const fullPath = path.join(API_DIR, file);
  const lines    = fs.readFileSync(fullPath, 'utf8').split('\n');

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i];

    // Strip everything from // onward (covers both comment-only lines and
    // inline comments; also handles CRLF since \r lands after the comment).
    // Edge-case note: our path strings never contain //, so this is safe.
    const codePart = raw.split('//')[0];

    // Nothing left after stripping – pure comment or blank line, skip.
    if (!codePart.trim()) continue;

    const lineNum = i + 1;

    // ── Rule A: /organizations/ without /v1/organizations/  ───────────────
    if (RE_BARE_ORG.test(codePart) && !RE_V1_ORG.test(codePart)) {
      console.error(`  ${file}:${lineNum}: path missing /v1 prefix (Rule A):`);
      console.error(`    ${raw.trim()}`);
      errors++;
    }

    // ── Rule B: apiClient call whose path arg starts with / but not /v1/ ──
    const m = RE_API_CALL.exec(codePart);
    if (m) {
      // m[1] starts with "/" – extract just the leading path segment
      // (stop at ${ template expression or closing quote/backtick).
      const pathStart = m[1].replace(/[`"'].*$/, '').replace(/\$\{.*/, '');
      if (pathStart && !RE_GOOD_PATH.test(pathStart)) {
        console.error(`  ${file}:${lineNum}: path missing /v1 prefix (Rule B):`);
        console.error(`    ${raw.trim()}`);
        errors++;
      }
    }
  }
}

if (errors > 0) {
  console.error(`\n[check-api-paths] FAIL: ${errors} path(s) missing /v1 prefix. Fix them in frontend/src/lib/api/.`);
  process.exit(1);
}

console.log(`[check-api-paths] Scanned ${files.length} file(s) – all API paths have /v1 prefix. OK`);
