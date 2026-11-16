import { Button as AstryxButton } from "@astryxdesign/core/Button";
import { Children, createContext, isValidElement, type ButtonHTMLAttributes, type ReactNode } from "react";

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "default" | "primary" | "ghost" | "danger" | "link" | "row";
  size?: "sm" | "md" | "lg" | "icon";
  loading?: boolean;
  width?: number | string;
};

export const DefaultButtonSize = createContext<ButtonProps["size"]>(undefined);

function textOf(node: ReactNode): string {
  return Children.toArray(node)
    .map(child => typeof child === "string" || typeof child === "number" ? String(child) : isValidElement<{ children?: ReactNode }>(child) ? textOf(child.props.children) : "")
    .join(" ")
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * Platform button facade over Astryx Button.
 *
 * The public API stays unchanged for the hundreds of existing call sites:
 * `variant` accepts the legacy default/primary/ghost/danger/link/row set,
 * `size` accepts sm/md/lg/icon, `loading` maps to Astryx `isLoading`.
 */
export function Button({
  className,
  variant = "default",
  size,
  type = "button",
  title,
  loading = false,
  disabled,
  children,
  ...props
}: ButtonProps) {
  const blocked = disabled || loading;
  const isIconOnly = size === "icon";
  const parts = Children.toArray(children);
  const leadingIcon = !isIconOnly && parts.length > 1 && isValidElement(parts[0]) && typeof parts[0].type !== "string" ? parts[0] : undefined;
  const content = leadingIcon ? parts.slice(1) : children;
  const label =
    (typeof props["aria-label"] === "string" && props["aria-label"]) ||
    textOf(children) ||
    title ||
    "Button";
  const astryxVariant =
    variant === "primary"
      ? "primary"
      : variant === "danger"
        ? "destructive"
        : variant === "ghost" || variant === "link" || variant === "row"
          ? "ghost"
          : "secondary";
  const linkClass =
    variant === "link"
      ? "!h-auto !min-h-0 !border-0 !bg-transparent !px-0 !py-0 !shadow-none font-normal underline hover:!bg-transparent"
      : variant === "row"
        ? "!h-auto !min-h-0 !w-full !justify-start !border-0 !bg-transparent !px-0 !py-0 !shadow-none font-normal hover:!bg-transparent"
        : "";

  return (
    <AstryxButton
      {...props}
      data-ui="button"
      data-variant={variant}
      label={label}
      variant={astryxVariant}
      size={isIconOnly ? "sm" : (size ?? undefined)}
      isDisabled={blocked}
      isLoading={loading}
      isIconOnly={isIconOnly}
      icon={isIconOnly ? children : leadingIcon}
      tooltip={title}
      type={type}
      className={[linkClass, className].filter(Boolean).join(" ") || undefined}
    >
      {isIconOnly ? undefined : content}
    </AstryxButton>
  );
}
