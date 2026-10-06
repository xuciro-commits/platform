import { cva, type VariantProps } from "class-variance-authority";
import type { ButtonHTMLAttributes } from "react";
import { cn } from "../lib/cn";

const button = cva(
  "inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md border text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-3.5",
  {
    variants: {
      variant: {
        default: "border-border bg-surface hover:bg-row-hover",
        primary: "border-primary bg-primary text-primary-foreground hover:opacity-90",
        ghost: "border-transparent hover:bg-row-hover",
        danger: "border-border bg-surface text-[var(--tone-danger)] hover:bg-row-hover",
        /** Opens something (a record, a document) from running text. */
        link: "border-transparent font-normal underline hover:opacity-80",
        /** One row of a list that opens what it names. */
        row: "w-full justify-start border-transparent font-normal hover:bg-row-hover",
      },
      size: { sm: "h-6 px-2", md: "h-7 px-2.5", icon: "size-7" },
    },
    compoundVariants: [{ variant: "link", class: "h-auto px-0" }, { variant: "row", class: "h-auto px-2 py-1 text-left" }],
    defaultVariants: { variant: "default", size: "md" },
  },
);

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & VariantProps<typeof button>;

export function Button({ className, variant, size, type = "button", ...props }: ButtonProps) {
  // `data-ui`/`data-variant` are the skin hooks (themes/industrial.css); they carry no behaviour.
  return <button type={type} data-ui="button" data-variant={variant ?? "default"} className={cn(button({ variant, size }), className)} {...props} />;
}
