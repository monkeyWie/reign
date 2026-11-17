"use client";
import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { clsx } from "clsx";
import { twMerge } from "tailwind-merge";
export const cn = (...args: Parameters<typeof clsx>) => twMerge(clsx(...args));
const variants = cva("button", {
  variants: {
    variant: {
      default: "",
      primary: "primary",
      ghost: "ghost",
      destructive: "destructive",
    },
    size: { default: "", icon: "icon", small: "small" },
  },
  defaultVariants: { variant: "default", size: "default" },
});
export function Button({
  className,
  variant,
  size,
  children,
  disabled,
  onClick,
  ...props
}: Omit<React.ComponentProps<"button">, "onClick"> &
  VariantProps<typeof variants> & {
    onClick?: (
      event: React.MouseEvent<HTMLButtonElement>,
    ) => void | Promise<void>;
  }) {
  const [pending, setPending] = React.useState(false);
  const inFlight = React.useRef(false);
  const focusTarget = React.useRef<HTMLButtonElement | null>(null);
  React.useLayoutEffect(() => {
    if (pending) return;
    const button = focusTarget.current;
    focusTarget.current = null;
    if (
      button?.isConnected &&
      !button.disabled &&
      document.activeElement === document.body
    ) {
      button.focus();
    }
  }, [pending]);
  return (
    <button
      type="button"
      {...props}
      className={cn(variants({ variant, size }), className)}
      disabled={disabled || pending}
      aria-busy={pending || props["aria-busy"]}
      onClick={(event) => {
        if (inFlight.current || disabled) return;
        inFlight.current = true;
        const button = event.currentTarget;
        const hadFocus = document.activeElement === button;
        try {
          const result = onClick?.(event);
          if (result && typeof result.then === "function") {
            focusTarget.current = hadFocus ? button : null;
            setPending(true);
            // Callers own error presentation; keep this control pending until
            // their entire request and any refresh have finished.
            void result.finally(() => {
              inFlight.current = false;
              setPending(false);
            });
          } else {
            inFlight.current = false;
          }
        } catch (error) {
          inFlight.current = false;
          throw error;
        }
      }}
    >
      <span className="button-content" style={{ opacity: pending ? 0 : 1 }}>
        {children}
      </span>
      {pending && <span className="button-spinner" aria-hidden="true" />}
    </button>
  );
}
