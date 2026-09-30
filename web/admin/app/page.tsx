// Home page of the admin dashboard (`/`): names the app in a shared web/ui Card. A Server
// Component rendered at build time; it has no data, state or client-side JavaScript of its own.
import { Card, CardDescription, CardHeader, CardTitle } from '@dbcloud/ui/card';

/** Renders the home page. No props (App Router page without params). */
export default function HomePage() {
  return (
    <main className="mx-auto flex min-h-screen max-w-2xl items-center p-6">
      <Card aria-labelledby="page-title" className="w-full">
        <CardHeader>
          <CardTitle>
            <h1 id="page-title">dbcloud admin</h1>
          </CardTitle>
          <CardDescription>Staff administration dashboard.</CardDescription>
        </CardHeader>
      </Card>
    </main>
  );
}
