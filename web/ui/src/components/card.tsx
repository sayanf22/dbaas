// Card: a bordered surface that groups related content (shadcn/ui Card, trimmed to the parts the
// apps use). Presentational only: plain elements, no state and no effects, so it renders in Server
// and Client Components alike. Headings are passed in by the caller, which owns the page outline.
import type { ComponentProps } from 'react';

import { cn } from '../lib/cn';

/**
 * Card container. Props: any `<section>` attributes; `className` is merged over the defaults.
 * A `<section>` so a labelled card (`aria-labelledby`) is a landmark for assistive technology.
 */
export function Card({ className, ...props }: ComponentProps<'section'>) {
  return (
    <section
      data-slot="card"
      className={cn('bg-card text-card-foreground rounded-xl border shadow-sm', className)}
      {...props}
    />
  );
}

/** Card header: stacks the title and description. Props: any `<div>` attributes. */
export function CardHeader({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="card-header" className={cn('grid gap-2 p-6', className)} {...props} />;
}

/**
 * Card title wrapper. Props: any `<div>` attributes; children are usually a heading element
 * (`<h1>`–`<h6>`) chosen by the caller for the correct document outline.
 */
export function CardTitle({ className, ...props }: ComponentProps<'div'>) {
  return (
    <div
      data-slot="card-title"
      className={cn('text-2xl leading-tight font-semibold', className)}
      {...props}
    />
  );
}

/** Card description: secondary text under the title. Props: any `<p>` attributes. */
export function CardDescription({ className, ...props }: ComponentProps<'p'>) {
  return (
    <p
      data-slot="card-description"
      className={cn('text-muted-foreground text-sm', className)}
      {...props}
    />
  );
}
