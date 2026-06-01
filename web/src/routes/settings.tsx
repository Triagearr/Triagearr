import { createFileRoute, Link, Outlet } from "@tanstack/react-router";
import { Bell, Database, Download, Gauge, Info, Link2, Power, Settings, Shield, SlidersHorizontal, Timer } from "lucide-react";
import { m } from "@/paraglide/messages";

const sections = [
  { to: "/settings/mode",                       label: m.settings_nav_mode(),                Icon: Power },
  { to: "/settings/arr-connections",            label: m.settings_nav_arr_connections(),     Icon: Link2 },
  { to: "/settings/torrent-client-connections", label: m.settings_nav_torrent_connections(), Icon: Download },
  { to: "/settings/scoring",         label: m.settings_nav_scoring(),          Icon: SlidersHorizontal },
  { to: "/settings/polling",         label: m.settings_nav_polling(),          Icon: Timer },
  { to: "/settings/disk-pressure",   label: m.settings_nav_disk_pressure(),    Icon: Gauge },
  { to: "/settings/notifications",   label: m.settings_nav_notifications(),    Icon: Bell },
  { to: "/settings/security",        label: m.settings_nav_security(),         Icon: Shield },
  { to: "/settings/debug",           label: m.settings_nav_effective_config(), Icon: Database },
  { to: "/settings/about",           label: m.settings_nav_about(),            Icon: Info },
] as const;

function SettingsLayout() {
  return (
    <div style={{ display: "contents" }}>
      {/* Topbar */}
      <div className="topbar">
        <Settings size={15} style={{ color: "var(--fg-3)" }} />
        <div className="topbar-title">{m.settings_title()}</div>
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
