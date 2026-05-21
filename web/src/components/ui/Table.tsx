import type { HTMLAttributes, TableHTMLAttributes, TdHTMLAttributes, ThHTMLAttributes } from "react";
import { cn } from "@/lib/cn";

export function Table({ className, ...p }: TableHTMLAttributes<HTMLTableElement>) {
  return (
    <div className="relative w-full overflow-auto">
      <table className={cn("w-full text-sm", className)} {...p} />
    </div>
  );
}
export function THead({ className, ...p }: HTMLAttributes<HTMLTableSectionElement>) {
  return <thead className={cn("[&_tr]:border-b", className)} {...p} />;
}
export function TBody({ className, ...p }: HTMLAttributes<HTMLTableSectionElement>) {
  return <tbody className={cn("[&_tr:last-child]:border-0", className)} {...p} />;
}
export function TR({ className, ...p }: HTMLAttributes<HTMLTableRowElement>) {
  return (
    <tr className={cn("border-b border-border transition-colors hover:bg-muted/40", className)} {...p} />
  );
}
export function TH({ className, ...p }: ThHTMLAttributes<HTMLTableCellElement>) {
  return (
    <th
      className={cn(
        "h-9 px-3 text-left align-middle font-medium text-muted-foreground text-xs uppercase tracking-wide",
        className,
      )}
      {...p}
    />
  );
}
export function TD({ className, ...p }: TdHTMLAttributes<HTMLTableCellElement>) {
  return <td className={cn("px-3 py-2 align-middle", className)} {...p} />;
}
