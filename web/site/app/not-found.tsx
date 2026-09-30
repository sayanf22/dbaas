// Not-found page of the public site, exported as out/404.html, which Cloudflare serves for unknown
// paths (`not_found_handling = "404-page"`). It replaces Next.js's default page because that one
// uses inline styles, which the CSP (`default-src 'self'`) blocks. Server Component, build time.
import { Card, CardDescription, CardHeader, CardTitle } from '@dbcloud/ui/card';

/** Renders the 404 page. No props (App Router not-found file). */
export default function NotFound() {
  return (
    <main className="mx-auto flex min-h-screen max-w-2xl items-center p-6">
      <Card aria-labelledby="page-title" className="w-full">
        <CardHeader>
          <CardTitle>
            <h1 id="page-title">Page not found</h1>
          </CardTitle>
          <CardDescription>This page doesn&apos;t exist.</CardDescription>
        </CardHeader>
      </Card>
    </main>
  );
}
