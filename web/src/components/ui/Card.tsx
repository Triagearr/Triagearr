import type { HTMLAttributes } from "react";
import { cn } from "@/lib/cn";

export function Card({ className, ...p }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "rounded-lg border bg-card text-card-foreground shadow-sm",
        className,
      )}
      {...p}
    />
  );
}

export function CardHeader({ className, ...p }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex flex-col space-y-1 p-5", className)} {...p} />;
}

export function CardTitle({ className, ...p }: HTMLAttributes<HTMLHeadingElement>) {
  return <h3 className={cn("font-semibold leading-none tracking-tight text-base", className)} {...p} />;
}

export function CardDescription({ className, ...p }: HTMLAttributes<HTMLParagraphElement>) {
  return <p className={cn("text-sm text-muted-foreground", className)} {...p} />;
}

export function CardContent({ className, ...p }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("px-5 pb-5", className)} {...p} />;
}

export function CardFooter({ className, ...p }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex items-center px-5 pb-5", className)} {...p} />;
}
