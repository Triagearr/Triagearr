import { useEffect, type ReactNode } from "react";
import { cn } from "@/lib/cn";

type Props = {
  open: boolean;
  onClose: () => void;
  title?: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  className?: string;
};

export function Modal({ open, onClose, title, description, children, className }: Props) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div
        className="absolute inset-0 bg-black/50 backdrop-blur-sm"
        aria-hidden="true"
        onClick={onClose}
      />
      <div
        role="dialog"
        aria-modal="true"
        className={cn(
          "relative z-10 w-full max-w-lg rounded-lg border border-border bg-card text-card-foreground shadow-xl p-6",
          className,
        )}
      >
        {title && <h2 className="text-lg font-semibold mb-1">{title}</h2>}
        {description && <p className="text-sm text-muted-foreground mb-4">{description}</p>}
        {children}
      </div>
    </div>
  );
}

type DrawerProps = {
  open: boolean;
  onClose: () => void;
  title?: ReactNode;
  children: ReactNode;
  className?: string;
};

export function Drawer({ open, onClose, title, children, className }: DrawerProps) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div className="fixed inset-0 z-40">
      <div className="absolute inset-0 bg-black/40" onClick={onClose} aria-hidden="true" />
      <div
        role="dialog"
        aria-modal="true"
        className={cn(
          "absolute right-0 top-0 h-full w-full max-w-xl bg-card border-l border-border shadow-xl overflow-y-auto",
          className,
        )}
      >
        {title && (
          <div className="px-6 py-4 border-b border-border sticky top-0 bg-card z-10">
            <h2 className="text-lg font-semibold">{title}</h2>
          </div>
        )}
        <div className="px-6 py-5">{children}</div>
      </div>
    </div>
  );
}
