import { createRootRoute, Link, Outlet } from "@tanstack/react-router";
import { Activity, Gauge, ListChecks, Menu, Settings, X } from "lucide-react";
import { useEffect, useState } from "react";
import { cn } from "@/lib/cn";
import { useVersion } from "@/api/hooks";
import { Logo } from "@/components/Logo";

// TanStack Router navigates via pushState, which doesn't fire `popstate`, so a
// popstate listener for closing the drawer would be dead code. The Link
// onClick handler below closes the drawer on navigation instead.

const nav = [
  { to: "/", label: "Dashboard", Icon: Gauge },
  { to: "/torrents", label: "Torrents", Icon: ListChecks },
  { to: "/actions", label: "Actions", Icon: Activity },
  { to: "/settings", label: "Settings", Icon: Settings },
];

function Layout() {
  const version = useVersion();
  const [drawerOpen, setDrawerOpen] = useState(false);

  useEffect(() => {
    if (drawerOpen) {
      const prev = document.body.style.overflow;
      document.body.style.overflow = "hidden";
      return () => {
        document.body.style.overflow = prev;
      };
    }
  }, [drawerOpen]);

  return (
    <div className="min-h-screen flex flex-col md:flex-row">
      {/* Mobile top bar */}
      <header className="md:hidden sticky top-0 z-20 flex items-center justify-between gap-3 border-b border-border bg-card px-4 h-14">
        <button
          aria-label="Open menu"
          onClick={() => setDrawerOpen(true)}
          className="h-10 w-10 inline-flex items-center justify-center rounded-md hover:bg-accent"
        >
          <Menu className="h-5 w-5" />
        </button>
        <div className="flex items-center gap-2 font-semibold tracking-tight">
          <Logo size={28} />
          Triagearr
        </div>
        <div className="text-xs text-muted-foreground font-mono">
          {version.data?.version ?? "…"}
        </div>
      </header>

      {/* Mobile drawer */}
      {drawerOpen && (
        <button
          aria-label="Close menu"
          className="md:hidden fixed inset-0 z-30 bg-foreground/40"
          onClick={() => setDrawerOpen(false)}
        />
      )}
      <aside
        className={cn(
          // Mobile: off-canvas drawer.
          "md:static fixed inset-y-0 left-0 z-40 w-64 max-w-[85vw] bg-card border-r border-border flex flex-col transition-transform",
          drawerOpen ? "translate-x-0" : "-translate-x-full",
          // Desktop: visible, no transform.
          "md:translate-x-0 md:w-60 md:shrink-0",
        )}
      >
        <div className="px-5 py-5 border-b border-border flex items-center justify-between">
          <div className="flex items-center gap-3">
            <Logo size={36} />
            <div>
              <div className="text-lg font-semibold tracking-tight">Triagearr</div>
              <div className="text-xs text-muted-foreground">
                {version.data?.version ?? "loading…"}
              </div>
            </div>
          </div>
          <button
            aria-label="Close menu"
            className="md:hidden h-9 w-9 inline-flex items-center justify-center rounded-md hover:bg-accent"
            onClick={() => setDrawerOpen(false)}
          >
            <X className="h-5 w-5" />
          </button>
        </div>
        <nav className="flex-1 p-2 flex flex-col gap-1">
          {nav.map(({ to, label, Icon }) => (
            <Link
              key={to}
              to={to}
              onClick={() => setDrawerOpen(false)}
              className="flex items-center gap-3 rounded-md px-3 py-3 md:py-2 text-sm text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors [&.active]:bg-accent [&.active]:text-accent-foreground"
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

      <main className="flex-1 min-w-0 overflow-y-auto">
        <div className="mx-auto w-full max-w-screen-2xl">
          <Outlet />
        </div>
      </main>
    </div>
  );
}

export const Route = createRootRoute({ component: Layout });
