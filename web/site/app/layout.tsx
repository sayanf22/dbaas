// Root layout of the public site: the <html>/<body> shell, document language and default metadata.
// A Server Component rendered at build time; it loads no fonts or scripts from other origins.
import type { Metadata } from 'next';
import type { ReactNode } from 'react';

import './globals.css';

/** Default <title> and description for every page of the public site. */
export const metadata: Metadata = {
  title: 'dbcloud',
  description: 'dbcloud managed PostgreSQL databases.',
};

/** Wraps every page. Props: `children`, the page rendered by the App Router. */
export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en">
      <body className="antialiased">{children}</body>
    </html>
  );
}
