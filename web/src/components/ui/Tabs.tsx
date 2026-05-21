import { useState, type ReactNode } from "react";
import { cn } from "@/lib/cn";

type TabsProps = {
  tabs: { id: string; label: ReactNode; content: ReactNode }[];
  initial?: string;
  className?: string;
};

export function Tabs({ tabs, initial, className }: TabsProps) {
  const [active, setActive] = useState(initial ?? tabs[0]?.id);
  return (
    <div className={cn("flex flex-col gap-4", className)}>
      <div className="flex gap-1 border-b border-border">
        {tabs.map((t) => (
          <button
            key={t.id}
            onClick={() => setActive(t.id)}
            className={cn(
              "px-3 py-2 text-sm font-medium border-b-2 -mb-px transition-colors",
              active === t.id
                ? "border-primary text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground",
            )}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div>{tabs.find((t) => t.id === active)?.content}</div>
    </div>
  );
}
