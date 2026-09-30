// Root layout of the admin dashboard: the <html>/<body> shell, document language and default
// metadata. A Server Component rendered at build time; it loads nothing from other origins.
import type { Metadata } from 'next';
import type { ReactNode } from 'react';

import './globals.css';

/** Default <title> and description for every page of the admin dashboard. */
export const metadata: Metadata = {
  title: 'dbcloud admin',
  description: 'The dbcloud staff administration dashboard.',
};

/** Wraps every page. Props: `children`, the page rendered by the App Router. */
export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en">
      <body className="antialiased">{children}</body>
    </html>
  );
}
