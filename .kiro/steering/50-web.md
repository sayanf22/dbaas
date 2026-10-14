---
inclusion: fileMatch
fileMatchPattern: 'web/**'
---

# Web rules (Next.js 16 static exports: `web/site`, `web/customer`, `web/admin`, shared `web/ui`)

## Next.js setup
- MUST: App Router with `output: 'export'`, `images: { unoptimized: true }` and `trailingSlash` left at the default. The build output (`out/`) is plain files served by Cloudflare Workers Static Assets (`assets.directory = "out"`, `not_found_handling = "404-page"`).
- MUST: no server features: no Route Handlers that read the request, no `proxy.ts`/middleware, no Server Actions, no ISR, no `cookies()`/`headers()`, no rewrites/redirects/headers in `next.config` (use `public/_headers` and `public/_redirects`). If a task seems to need one, stop and raise it (ADR-021).
- MUST: Server Components only render build-time content (layouts, marketing and docs pages). Everything that needs the user, the session or control-api is a Client Component (`'use client'`).
- MUST: pages for one resource read the ID from a query parameter (`/databases/detail?id=…`) with `useSearchParams()` inside `<Suspense>`; never a dynamic segment for runtime IDs, because a static export can't prerender them.
- MUST: Next.js stays on the newest 16.3.x patch (≥ 16.3.7) and React ≥ 19.2.6; security releases are applied within a week even though no Next.js server runs in production.
- MUST: ESLint flat config with `eslint-config-next` (`/core-web-vitals`, `/typescript`); `next lint` no longer exists.
- MUST: production builds use `next build --webpack` while Turbopack breaks Trusted Types; local dev may use Turbopack.

## Structure
- MUST: TypeScript `strict` (plus `noUncheckedIndexedAccess`); no `any` without a `// reason:` comment.
- MUST: talk to control-api only through the `openapi-fetch` client generated from `api/openapi.yaml`. supabase-js (browser client) is used for sign-in only, never for data access; `@supabase/ssr` isn't used because there is no server.
- MUST: server state via TanStack Query v5 (one `QueryClientProvider` in a client `providers.tsx`) with explicit loading, empty and error states. Long operations poll `/v1/operations/{id}` with backoff and stop on terminal states.
- MUST: UI from shadcn/ui components in `web/ui` (Radix primitives, Tailwind CSS v4); app code composes them rather than copying variants.
- SHOULD: components stay small and presentational; data fetching lives in hooks.
- MUST: every component and hook has a short header comment explaining its purpose and props.

## Security
- MUST: no secrets in the bundle. Only public values (API base URL, Supabase URL + publishable key, Turnstile site key) come from `NEXT_PUBLIC_*` build-time env; CI fails if a variable name matches a secret pattern.
- MUST: render text with JSX, never through `innerHTML`, `outerHTML`, `document.write` or `insertAdjacentHTML`. `dangerouslySetInnerHTML` is allowed only for HTML sanitized with DOMPurify in the same module, and the `{ __html }` object is created where the sanitizing happens.
- MUST: URLs placed in `href`/`src` from data are parsed with `new URL()` and allowed only for `https:` (and `http:` on localhost in development); React doesn't block `javascript:` URLs.
- MUST: no `eval`, `new Function`, or string arguments to `setTimeout`/`setInterval`; no `next/script` with `beforeInteractive` or inline content.
- MUST: security headers come from `public/_headers` on every route: HSTS, `Referrer-Policy: strict-origin-when-cross-origin`, `X-Content-Type-Options: nosniff`, `Permissions-Policy` with unused features off, and a strict CSP: `default-src 'self'`; `script-src 'self'` plus the SHA-256 hash of every inline script (a post-build step hashes the inline `self.__next_f.push(...)` scripts Next.js emits and writes them into `_headers`; the build fails if a rule exceeds Cloudflare's 2,000-character line or 100-rule limits); `connect-src` limited to `api.example.com` and the Supabase project URL; `frame-ancestors 'none'`; `base-uri 'none'`; `object-src 'none'`; `require-trusted-types-for 'script'`. CSP changes ship as Report-Only first. If Trusted Types can't be enforced with the pinned Next.js version, it stays Report-Only and the gap is recorded in ADR-021.
- MUST: no third-party scripts, fonts or analytics loaded from other origins (self-host fonts with `next/font/local`).
- MUST: sessions: supabase-js keeps the Supabase session in browser storage, which any XSS could read. That is accepted only because of the CSP and Trusted Types above, no third-party scripts, short-lived access tokens and Supabase refresh-token rotation. Our own code never copies tokens into other storage, URLs or logs. If any of those controls is removed, move to a backend-for-frontend with `__Host-` HttpOnly cookies (RFC 10017).
- MUST: API calls send the access token in the `Authorization` header (no cookies), so they aren't exposed to CSRF; control-api's CORS allows only the web app origins, never `*` or a reflected origin.
- MUST: destructive actions (delete DB, purge, restore, rotate credentials, approve MCP writes, cancel subscription) require an explicit confirmation step that names the resource.
- MUST: connection strings and passwords are shown only on explicit reveal, never logged to the console, and copied with the Clipboard API.
- MUST: `web/admin` is reachable only behind Cloudflare Access; it holds no secrets and uses the same control-api authorization as everyone else.

## Accessibility
- MUST: accessible UI: semantic HTML, labelled controls, keyboard navigation, visible focus, colour contrast; Radix primitives via shadcn/ui; Playwright + axe checks in CI. Automated checks don't prove WCAG conformance; accessibility-critical flows also get manual review with assistive technology.

## Dependencies (pnpm)
- MUST: `pnpm-lock.yaml` committed and installs use `--frozen-lockfile`.
- MUST: dependency lifecycle scripts are off by default; only packages listed in the workspace's build allow-list may run them.
- MUST: `minimumReleaseAge` delays brand-new package versions (at least 1 day), and exotic sub-dependencies (git/tarball URLs) are blocked.
- MUST: Trivy scans the lockfile in CI; high or critical findings block the merge.
