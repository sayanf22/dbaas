// ESLint flat config for the admin dashboard: eslint-config-next core-web-vitals + typescript
// presets (.kiro/steering/50-web.md). Build output and Next.js-generated files are not linted.
import { defineConfig, globalIgnores } from 'eslint/config';
import nextVitals from 'eslint-config-next/core-web-vitals';
import nextTs from 'eslint-config-next/typescript';

export default defineConfig([
  ...nextVitals,
  ...nextTs,
  globalIgnores(['.next/**', 'out/**', 'next-env.d.ts']),
]);
