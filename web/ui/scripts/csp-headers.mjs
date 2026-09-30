#!/usr/bin/env node
// @ts-check
/*
 * Post-build CSP generator for a Next.js static export (.kiro/steering/50-web.md "Security",
 * plan/02-technology-stack.md §8). Run from an app directory right after `next build --webpack`.
 *
 * What it does:
 *   1. Reads every .html file under ./out and computes the SHA-256 of each inline <script> (the
 *      `self.__next_f.push(...)` flight-data scripts and any other inline script Next.js emits).
 *   2. Appends one CSP line to the `/*` rule of ./out/_headers (copied there by Next.js from
 *      public/_headers, which holds the other security headers), listing every hash.
 *   3. Fails the build if any _headers line exceeds Cloudflare's 2,000-character limit or the file
 *      has more than 100 rules, or if the HTML contains inline styles the policy would block.
 *
 * Why one `/*` rule with the union of all hashes: Cloudflare comma-joins a header set by several
 * matching rules, which for CSP means several policies that must all pass. A single rule keeps
 * exactly one policy on every path, including arbitrary URLs answered with 404.html. When the
 * union outgrows 2,000 characters the build fails here and per-route rules become necessary.
 *
 * What it deliberately does not do: it doesn't parse arbitrary HTML (the input is our own build
 * output, scanned with a narrow regex), doesn't add hashes for inline event handlers or styles
 * (the build fails instead), and doesn't decide enforcement: the policy ships as Report-Only first,
 * as 50-web.md requires, until Trusted Types enforcement is verified with the pinned Next.js.
 */
import { createHash } from 'node:crypto';
import { readdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

/** Next.js static export directory, relative to the app directory the script runs in. */
const OUT_DIR = 'out';

/** 2,000 characters per line, whole line including indentation: Cloudflare static assets limit. */
const MAX_LINE_CHARS = 2000;

/** 100 header rules per _headers file: Cloudflare static assets limit. */
const MAX_RULES = 100;

/**
 * Header name used while the policy is being proven (50-web.md: CSP changes ship as Report-Only
 * first). Switching to `Content-Security-Policy` is the enforcement step.
 */
const CSP_HEADER = 'Content-Security-Policy-Report-Only';

/**
 * Inline scripts: a <script> start tag without a `src` attribute, and its body. Next.js never
 * emits `</script` inside a script body (it escapes it), so the lazy match ends at the right tag.
 */
const INLINE_SCRIPT = /<script(?![^>]*\ssrc=)[^>]*>([\s\S]*?)<\/script>/gi;

/** Any <script> element, used to strip script bodies before looking for inline styles. */
const ANY_SCRIPT = /<script\b[^>]*>[\s\S]*?<\/script>/gi;

/** Inline styles blocked by `default-src 'self'`: <style> elements and style="" attributes. */
const INLINE_STYLE = /<style[\s>]|\sstyle=/i;

/**
 * Prints the reason and exits non-zero so the app's `build` script fails.
 * @param {string} message
 * @returns {never}
 */
function fail(message) {
  console.error(`csp-headers: ${message}`);
  process.exit(1);
}

/**
 * Returns the CSP source expression for one inline script body. Browsers hash the script text
 * after HTML input-stream preprocessing, which turns CRLF and lone CR into LF, so we do the same.
 * @param {string} body
 * @returns {string}
 */
function hashSource(body) {
  const normalized = body.replace(/\r\n?/g, '\n');
  return `'sha256-${createHash('sha256').update(normalized, 'utf8').digest('base64')}'`;
}

/**
 * Collects the sorted, de-duplicated hashes of all inline scripts in the export and checks that
 * no page relies on inline styles.
 * @returns {Promise<string[]>}
 */
async function collectScriptHashes() {
  const entries = await readdir(OUT_DIR, { recursive: true, withFileTypes: true });
  const htmlFiles = entries
    .filter((entry) => entry.isFile() && entry.name.endsWith('.html'))
    .map((entry) => join(entry.parentPath, entry.name));
  if (htmlFiles.length === 0) fail(`no .html files under ${OUT_DIR}/; run next build first`);

  /** @type {Set<string>} */
  const hashes = new Set();
  for (const file of htmlFiles) {
    const html = await readFile(file, 'utf8');
    for (const match of html.matchAll(INLINE_SCRIPT)) hashes.add(hashSource(match[1] ?? ''));
    if (INLINE_STYLE.test(html.replace(ANY_SCRIPT, ''))) {
      fail(`${file} contains an inline style, which the CSP blocks; use Tailwind classes instead`);
    }
  }
  return [...hashes].sort();
}

/**
 * Builds the policy (50-web.md). connect-src is 'self' only: Next.js fetches the exported RSC
 * payloads from its own origin, and no app calls control-api or Supabase yet; the customer and
 * admin apps add api.example.com and the Supabase project URL when they start to (Step 0.11).
 * @param {string[]} scriptHashes
 * @returns {string}
 */
function buildPolicy(scriptHashes) {
  return [
    "default-src 'self'",
    ["script-src 'self'", ...scriptHashes].join(' '),
    "connect-src 'self'",
    "object-src 'none'",
    "base-uri 'none'",
    "frame-ancestors 'none'",
    "require-trusted-types-for 'script'",
  ].join('; ');
}

/**
 * Appends the CSP to the `/*` rule in out/_headers and enforces Cloudflare's limits. Not
 * idempotent on purpose: a second run on the same export fails because the CSP is already there.
 * @param {string} policy
 */
async function writeHeaders(policy) {
  const path = join(OUT_DIR, '_headers');
  const lines = (await readFile(path, 'utf8')).trimEnd().split('\n');

  // A rule starts with an unindented, non-comment line (the URL pattern).
  const rules = lines.filter((line) => line !== '' && !/^\s/.test(line) && !line.startsWith('#'));
  if (rules.at(-1) !== '/*') fail(`the last rule in ${path} must be "/*" (from public/_headers)`);
  if (lines.some((line) => /^\s*!?\s*content-security-policy/i.test(line))) {
    fail(`${path} already sets a CSP; this script is its only source`);
  }

  lines.push(`  ${CSP_HEADER}: ${policy}`);

  if (rules.length > MAX_RULES) fail(`${path} has ${rules.length} rules (limit ${MAX_RULES})`);
  for (const line of lines) {
    if (line.length > MAX_LINE_CHARS) {
      fail(`a line in ${path} has ${line.length} characters (limit ${MAX_LINE_CHARS})`);
    }
  }
  await writeFile(path, `${lines.join('\n')}\n`, 'utf8');
}

const hashes = await collectScriptHashes();
await writeHeaders(buildPolicy(hashes));
console.log(`csp-headers: wrote ${CSP_HEADER} with ${hashes.length} script hashes to ${OUT_DIR}/_headers`);
