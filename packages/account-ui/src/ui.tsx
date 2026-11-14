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
  ...props
}: React.ComponentProps<"button"> & VariantProps<typeof variants>) {
  return (
    <button className={cn(variants({ variant, size }), className)} {...props} />
  );
}
