import { cva, type VariantProps } from "class-variance-authority";
import type { HTMLAttributes } from "react";
import { cn } from "@/lib/cn";

const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium",
  {
    variants: {
      variant: {
        default: "bg-primary/15 text-primary border-primary/20",
        muted: "bg-muted text-muted-foreground border-transparent",
        success: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-300 border-emerald-500/30",
        warning: "bg-amber-500/15 text-amber-600 dark:text-amber-300 border-amber-500/30",
        destructive: "bg-destructive/15 text-destructive border-destructive/30",
        outline: "border-border text-muted-foreground",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

export interface BadgeProps
  extends HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...p }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...p} />;
}
