// Root layout of the customer dashboard: the <html>/<body> shell, document language and default
// metadata. A Server Component rendered at build time; it loads nothing from other origins.
import type { Metadata } from 'next';
import type { ReactNode } from 'react';

import './globals.css';

/** Default <title> and description for every page of the customer dashboard. */
export const metadata: Metadata = {
  title: 'dbcloud dashboard',
  description: 'The dbcloud customer dashboard.',
};

/** Wraps every page. Props: `children`, the page rendered by the App Router. */
export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en">
      <body className="antialiased">{children}</body>
    </html>
  );
}
