import { createRootRoute, Link, Outlet } from "@tanstack/react-router";
import { Activity, Gauge, ListChecks, Settings } from "lucide-react";
import { cn } from "@/lib/cn";
import { useVersion } from "@/api/hooks";

const nav = [
  { to: "/", label: "Dashboard", Icon: Gauge },
  { to: "/torrents", label: "Torrents", Icon: ListChecks },
  { to: "/actions", label: "Actions", Icon: Activity },
  { to: "/settings", label: "Settings", Icon: Settings },
];

function Layout() {
  const version = useVersion();
  return (
    <div className="flex min-h-screen">
      <aside className="w-56 shrink-0 border-r border-border bg-card flex flex-col">
        <div className="px-5 py-5 border-b border-border">
          <div className="text-lg font-semibold tracking-tight">Triagearr</div>
          <div className="text-xs text-muted-foreground">{version.data?.version ?? "loading…"}</div>
        </div>
        <nav className="flex-1 p-2 flex flex-col gap-1">
          {nav.map(({ to, label, Icon }) => (
            <Link
              key={to}
              to={to}
              className="flex items-center gap-3 rounded-md px-3 py-2 text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors [&.active]:bg-accent [&.active]:text-accent-foreground"
              activeOptions={{ exact: to === "/" }}
            >
              <Icon className="h-4 w-4" />
              <span>{label}</span>
            </Link>
          ))}
        </nav>
        <div className="px-5 py-4 border-t border-border text-xs text-muted-foreground">
          {version.data?.commit && (
            <div className="font-mono truncate" title={version.data.commit}>
              {version.data.commit.slice(0, 10)}
            </div>
          )}
        </div>
      </aside>
      <main className={cn("flex-1 overflow-y-auto")}>
        <Outlet />
      </main>
    </div>
  );
}

export const Route = createRootRoute({ component: Layout });
