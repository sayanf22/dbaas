// Class-name helper shared by the web/ui components (the shadcn/ui `cn` utility). It exists so a
// caller's `className` can override a component's default Tailwind classes without duplicates.
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

/**
 * Joins conditional class values (clsx) and resolves conflicting Tailwind utilities so the last one
 * wins (tailwind-merge). Pure and synchronous; never throws.
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
