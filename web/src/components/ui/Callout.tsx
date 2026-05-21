import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

type Variant = "destructive" | "warning";

const styles: Record<Variant, string> = {
  destructive: "border-destructive/30 bg-destructive/10 text-destructive",
  warning: "border-warning/30 bg-warning/10 text-warning-foreground",
};

export function Callout({
  children,
  variant = "destructive",
  className,
}: {
  children: ReactNode;
  variant?: Variant;
  className?: string;
}) {
  return (
    <div className={cn("rounded-md border p-3 text-sm", styles[variant], className)}>
      {children}
    </div>
  );
}
