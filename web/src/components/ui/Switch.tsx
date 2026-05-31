import { cn } from "@/lib/cn";

// Switch is the shared toggle primitive for settings rows. The danger variant
// (rose track when on) signals high-stakes toggles like arming live mode.
export function Switch({
  checked,
  onCheckedChange,
  disabled,
  variant = "primary",
}: {
  checked: boolean;
  onCheckedChange: (v: boolean) => void;
  disabled?: boolean;
  variant?: "primary" | "danger";
}) {
  return (
    <button
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => onCheckedChange(!checked)}
      className={cn(
        "relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50",
        checked ? (variant === "danger" ? "bg-rose-600" : "bg-primary") : "bg-input",
      )}
    >
      <span
        className={cn(
          "pointer-events-none inline-block h-5 w-5 rounded-full bg-background shadow-lg transition-transform",
          checked ? "translate-x-5" : "translate-x-0",
        )}
      />
    </button>
  );
}
