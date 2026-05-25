import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { Bell, Database, Gauge, Info, Link2, Settings2, Shield, SlidersHorizontal, Timer } from "lucide-react";

const sections = [
  { to: "/settings/arr-connections", label: "*arr connections", Icon: Link2 },
  { to: "/settings/scoring",         label: "Scoring",          Icon: SlidersHorizontal },
  { to: "/settings/polling",         label: "Polling",          Icon: Timer },
  { to: "/settings/disk-pressure",   label: "Disk pressure",    Icon: Gauge },
  { to: "/settings/notifications",   label: "Notifications",    Icon: Bell },
  { to: "/settings/security",        label: "Security",         Icon: Shield },
  { to: "/settings/debug",           label: "Effective config", Icon: Database },
  { to: "/settings/about",           label: "About",            Icon: Info },
] as const;

function SettingsLayout() {
  return (
    <div style={{ display: "contents" }}>
      {/* Topbar */}
      <div className="topbar">
        <Settings2 size={15} style={{ color: "var(--fg-3)" }} />
        <div className="topbar-title">Settings</div>
        <div className="topbar-sub">Live config — changes apply on save</div>
      </div>

      {/* Horizontal tab nav */}
      <div className="ds-tabs">
        {sections.map(({ to, label }) => (
          <Link
            key={to}
            to={to}
            activeProps={{ className: "ds-tab active" }}
            inactiveProps={{ className: "ds-tab" }}
          >
            {label}
          </Link>
        ))}
      </div>

      {/* Active section */}
      <div className="page">
        <Outlet />
      </div>
    </div>
  );
}

export const Route = createFileRoute("/settings")({ component: SettingsLayout });
