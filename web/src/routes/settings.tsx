import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import {
  Bell,
  Database,
  Gauge,
  Info,
  Settings2,
  Shield,
  SlidersHorizontal,
  Timer,
} from "lucide-react";
import { cn } from "@/lib/cn";

// Sidebar entries. Adding a settings section = one entry here + one route
// file (settings.<slug>.tsx). The layout stays untouched.
const sections = [
  { to: "/settings/scoring", label: "Scoring", Icon: SlidersHorizontal },
  { to: "/settings/polling", label: "Polling", Icon: Timer },
  { to: "/settings/disk-pressure", label: "Disk pressure", Icon: Gauge },
  { to: "/settings/notifications", label: "Notifications", Icon: Bell },
  { to: "/settings/security", label: "Security", Icon: Shield },
  { to: "/settings/debug", label: "Effective config", Icon: Database },
  { to: "/settings/about", label: "About", Icon: Info },
] as const;

function SettingsLayout() {
  return (
    <div className="p-4 sm:p-6 space-y-6">
      <header className="flex items-center gap-2">
        <Settings2 className="h-5 w-5 text-muted-foreground" />
        <h1 className="text-xl sm:text-2xl font-semibold tracking-tight">Settings</h1>
      </header>

      <div className="flex flex-col md:flex-row gap-6">
        {/* Section nav. Horizontal scroll on mobile, vertical rail on md+. */}
        <nav className="md:w-52 shrink-0">
          <ul className="flex md:flex-col gap-1 overflow-x-auto md:overflow-visible">
            {sections.map(({ to, label, Icon }) => (
              <li key={to}>
                <Link
                  to={to}
                  className={cn(
                    "flex items-center gap-2 rounded-md px-3 py-2 text-sm whitespace-nowrap",
                    "hover:bg-accent transition-colors",
                  )}
                  activeProps={{ className: "bg-accent font-medium text-foreground" }}
                  inactiveProps={{ className: "text-muted-foreground" }}
                >
                  <Icon className="h-4 w-4 shrink-0" />
                  {label}
                </Link>
              </li>
            ))}
          </ul>
        </nav>

        {/* Active section. */}
        <div className="flex-1 min-w-0 max-w-3xl space-y-6">
          <Outlet />
        </div>
      </div>
    </div>
  );
}

export const Route = createFileRoute("/settings")({ component: SettingsLayout });
