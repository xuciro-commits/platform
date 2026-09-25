import type { InputHTMLAttributes, SelectHTMLAttributes, TextareaHTMLAttributes } from "react";
import { cn } from "../lib/cn";

const field =
  "h-7 w-full rounded-md border border-border bg-surface px-2 text-sm outline-none focus-visible:border-ring focus-visible:outline-1 focus-visible:outline-ring aria-invalid:border-[var(--tone-danger)]";

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={cn(field, className)} {...props} />;
}

/** Native select: accessible, fast with long option lists, styled like Input. */
export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={cn(field, "pr-6", className)} {...props} />;
}

/** Several lines of text, styled like Input. */
export function Textarea({ className, ...props }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={cn(field, "h-auto min-h-16 py-1", className)} {...props} />;
}
