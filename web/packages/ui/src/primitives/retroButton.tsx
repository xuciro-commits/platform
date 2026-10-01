import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../lib/cn";

const retroButtonVariants = cva(
  "relative inline-flex items-center justify-center min-w-20 border-2 border-transparent rounded-[4px] bg-[#010101] shadow-[1px_1px_1px_rgba(255,255,255,0.6)] cursor-pointer select-none transition-all duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 disabled:cursor-not-allowed",
  {
    variants: {
      variant: {
        default: [
          "text-white",
          "[--bg-color:#f97316]",
          "[--bg-color-active:#ea580c]",
          "[--shadow-light:#fdba74]",
          "[--shadow-dark:#c2410c]",
        ],
        darkGray: [
          "text-white",
          "[--bg-color:#404040]",
          "[--bg-color-active:#262626]",
          "[--shadow-light:#737373]",
          "[--shadow-dark:#171717]",
        ],
        white: [
          "text-black",
          "[--bg-color:#e5e5e5]",
          "[--bg-color-active:#d4d4d4]",
          "[--shadow-light:#ffffff]",
          "[--shadow-dark:#737373]",
        ],
        lightGray: [
          "text-white",
          "[--bg-color:#737373]",
          "[--bg-color-active:#525252]",
          "[--shadow-light:#a3a3a3]",
          "[--shadow-dark:#404040]",
        ],
        gray: [
          "text-white",
          "[--bg-color:#525252]",
          "[--bg-color-active:#404040]",
          "[--shadow-light:#737373]",
          "[--shadow-dark:#262626]",
        ],
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

const retroButtonInnerVariants = cva(
  [
    "inline-block w-full rounded-[6px] px-3.5 py-1.5",
    "text-xs font-semibold uppercase tracking-wider text-center whitespace-nowrap",
    "bg-[var(--bg-color)] transition-all duration-150",
    "shadow-[inset_1px_1px_1px_var(--shadow-light),inset_-1px_-1px_1px_var(--shadow-dark),2px_2px_4px_rgba(0,0,0,0.8)]",
    "active:scale-[0.98] active:bg-[var(--bg-color-active)]",
    "active:shadow-[inset_0_0_4px_#000,inset_1px_1px_1px_transparent,inset_-1px_-1px_1px_transparent,2px_2px_4px_transparent]",
  ],
);

export interface RetroButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof retroButtonVariants> {
  children?: React.ReactNode;
}

export const RetroButton = React.forwardRef<HTMLButtonElement, RetroButtonProps>(
  ({ className, variant, children, type = "button", ...props }, ref) => {
    return (
      <button
        type={type}
        className={cn(retroButtonVariants({ variant, className }))}
        ref={ref}
        {...props}
      >
        <span className={retroButtonInnerVariants()}>{children}</span>
      </button>
    );
  },
);
RetroButton.displayName = "RetroButton";
