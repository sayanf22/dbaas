// ESLint flat config for the shared UI package: the same eslint-config-next presets as the apps
// (core-web-vitals + typescript, .kiro/steering/50-web.md) so shared components meet the app rules.
import { defineConfig } from 'eslint/config';
import nextVitals from 'eslint-config-next/core-web-vitals';
import nextTs from 'eslint-config-next/typescript';

export default defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    rules: {
      // reason: the rule checks <a> tags against a pages/ router; this package has no routes, so
      // it only prints a "Pages directory cannot be found" notice on every run.
      '@next/next/no-html-link-for-pages': 'off',
    },
  },
]);
