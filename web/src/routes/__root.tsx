import { createRootRoute, Link, Outlet } from "@tanstack/react-router";
import { Activity, Gauge, ListChecks, Menu, Moon, Settings, Sun, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useSummary } from "@/api/hooks";
import { Logo } from "@/components/Logo";
import { relativeTime } from "@/lib/format";
import { toggleTheme, resolveTheme, type Theme } from "@/lib/theme";
import { availableLocales, changeLocale, currentLocale, localeNames, type Locale } from "@/lib/locale";
import { m } from "@/paraglide/messages";

const nav = [
  { to: "/", label: () => m.nav_dashboard(), Icon: Gauge, exact: true },
  { to: "/torrents", label: () => m.nav_torrents(), Icon: ListChecks },
  { to: "/actions", label: () => m.nav_actions(), Icon: Activity },
  { to: "/settings", label: () => m.nav_settings(), Icon: Settings },
];

function Sidebar({ onClose }: { onClose?: () => void }) {
  const summary = useSummary();
  const data = summary.data;
  const [theme, setTheme] = useState<Theme>(resolveTheme);

  const arrs = data?.arrs ?? [];
  const healthyCount = arrs.filter((a) => a.healthy).length;
  const totalCount = arrs.length;
  const torrentClients = data?.torrent_clients ?? [];
  const torrentClientOk = torrentClients.length > 0 && torrentClients.every((c) => c.healthy);
  const volume = data?.volume;
  const usedPct =
    volume && Number(volume.total_bytes) > 0
      ? ((Number(volume.used_bytes) / Number(volume.total_bytes)) * 100).toFixed(1)
      : null;
  const isCritical =
    volume && usedPct != null
      ? Number(usedPct) >= 100 - (volume.threshold_free_percent ?? 0)
      : false;

  function handleThemeToggle() {
    const next = toggleTheme();
    setTheme(next);
  }

  return (
    <nav className="sidebar">
      <div className="sidebar-brand">
        <Logo size={30} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="sidebar-brand-name">Triagearr</div>
          <div className="sidebar-brand-sub" style={{ fontFamily: "'Geist Mono',ui-monospace,monospace" }}>
            {data ? m.status_connected() : "—"}
          </div>
        </div>
        {onClose && (
          <button onClick={onClose} aria-label={m.aria_close_menu()} className="btn btn-ghost btn-sm" style={{ padding: "0 6px" }}>
            <X size={15} />
          </button>
        )}
      </div>

      <div className="sidebar-nav">
        {nav.map(({ to, label, Icon, exact }) => (
          <Link
            key={to}
            to={to}
            onClick={onClose}
            activeOptions={{ exact: exact ?? false }}
            activeProps={{ className: "nav-item active" }}
            inactiveProps={{ className: "nav-item" }}
          >
            <Icon size={15} />
            <span>{label()}</span>
          </Link>
        ))}
      </div>

      <div className="sidebar-foot">
        <div className="sidebar-foot-row">
          <span>{m.sidebar_disk_pressure()}</span>
          <strong style={{ color: isCritical ? "var(--red-2)" : usedPct ? "var(--amber-2)" : undefined }}>
            {usedPct != null ? `${usedPct}%` : "—"}
          </strong>
        </div>
        <div className="sidebar-foot-row">
          <span>{m.sidebar_arrs_healthy()}</span>
          <strong>{totalCount > 0 ? `${healthyCount}/${totalCount}` : "—"}</strong>
        </div>
        <div className="sidebar-foot-row">
          <span>{m.sidebar_torrent_client()}</span>
          <span className={`dot ${torrentClientOk ? "green" : "red"}`} />
        </div>
        {data?.last_runs?.[0] && (
          <div className="sidebar-foot-row">
            <span>{m.sidebar_last_run()}</span>
            <strong>{relativeTime(data.last_runs[0].triggered_at)}</strong>
          </div>
        )}
        <div className="sidebar-foot-divider" />
        <div className="sidebar-foot-row">
          <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
            <span className={`dot ${data ? "green" : "red"}`} />
            {data ? m.status_daemon_up() : m.status_connecting()}
          </span>
          <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
            <select
              aria-label={m.aria_select_language()}
              value={currentLocale()}
              onChange={(e) => changeLocale(e.target.value as Locale)}
              className="btn btn-ghost btn-sm"
              style={{ height: 22, padding: "0 4px", fontSize: 11 }}
            >
              {availableLocales.map((loc) => (
                <option key={loc} value={loc}>
                  {localeNames[loc]}
                </option>
              ))}
            </select>
            <button
              onClick={handleThemeToggle}
              aria-label={m.aria_toggle_theme()}
              className="btn btn-ghost btn-sm"
              style={{ height: 22, padding: "0 5px" }}
            >
              {theme === "dark" ? <Sun size={13} /> : <Moon size={13} />}
            </button>
          </span>
        </div>
      </div>
    </nav>
  );
}

function Layout() {
  const [drawerOpen, setDrawerOpen] = useState(false);

  useEffect(() => {
    if (!drawerOpen) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => { document.body.style.overflow = prev; };
  }, [drawerOpen]);

  return (
    <div className="app-shell">
      {/* Desktop sidebar (hidden below lg via CSS .sidebar-desktop rule) */}
      <div className="sidebar-desktop">
        <Sidebar />
      </div>

      {/* Mobile: slide-in drawer + scrim */}
      {drawerOpen && (
        <>
          <div className="scrim" style={{ zIndex: 35 }} onClick={() => setDrawerOpen(false)} />
          <div className="sidebar-drawer">
            <Sidebar onClose={() => setDrawerOpen(false)} />
          </div>
        </>
      )}

      {/* Main content */}
      <main className="main">
        {/* Mobile topbar — visibility driven by .mobile-topbar in globals.css */}
        <div
          className="topbar mobile-topbar"
          style={{ position: "sticky", top: 0, zIndex: 20, paddingLeft: 10 }}
        >
          <button
            onClick={() => setDrawerOpen(true)}
            aria-label={m.aria_open_menu()}
            className="btn btn-ghost btn-sm"
            style={{ padding: "0 6px" }}
          >
            <Menu size={16} />
          </button>
          <Logo size={22} />
          <span style={{ fontWeight: 600, fontSize: 13, letterSpacing: "-0.01em" }}>Triagearr</span>
        </div>
        <Outlet />
      </main>
    </div>
  );
}

export const Route = createRootRoute({ component: Layout });
