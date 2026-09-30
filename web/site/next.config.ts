// Next.js configuration for the public site: a static export only (.kiro/steering/50-web.md,
// ADR-021). Deliberately no rewrites, redirects or headers here: those live in public/_headers
// and public/_redirects, which Cloudflare Workers Static Assets applies to the exported files.
import type { NextConfig } from 'next';

const nextConfig: NextConfig = {
  // Emit plain files to out/; no Next.js server runs in production.
  output: 'export',
  // The image optimizer needs a server; images are served as-is.
  images: { unoptimized: true },
  // web/ui ships TypeScript source, so Next.js compiles it with the app.
  transpilePackages: ['@dbcloud/ui'],
};

export default nextConfig;
