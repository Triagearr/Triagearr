import { createRootRoute, Link, Outlet } from "@tanstack/react-router";
import { Activity, Gauge, ListChecks, Menu, Moon, Settings, Sun, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useSummary } from "@/api/hooks";
import { Logo } from "@/components/Logo";
import { relativeTime } from "@/lib/format";
import { toggleTheme, resolveTheme, type Theme } from "@/lib/theme";

const nav = [
  { to: "/", label: "Dashboard", Icon: Gauge, exact: true },
  { to: "/torrents", label: "Torrents", Icon: ListChecks },
  { to: "/actions", label: "Actions", Icon: Activity },
  { to: "/settings", label: "Settings", Icon: Settings },
];

function Sidebar({ onClose }: { onClose?: () => void }) {
  const summary = useSummary();
  const data = summary.data;
  const [theme, setTheme] = useState<Theme>(resolveTheme);

  const arrs = data?.arrs ?? [];
  const healthyCount = arrs.filter((a) => a.healthy).length;
  const totalCount = arrs.length;
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
            {data ? "connected" : "—"}
          </div>
        </div>
        {onClose && (
          <button onClick={onClose} aria-label="Close menu" className="btn btn-ghost btn-sm" style={{ padding: "0 6px" }}>
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
            <span>{label}</span>
          </Link>
        ))}
      </div>

      <div className="sidebar-foot">
        <div className="sidebar-foot-row">
          <span>Disk pressure</span>
          <strong style={{ color: isCritical ? "var(--red-2)" : usedPct ? "var(--amber-2)" : undefined }}>
            {usedPct != null ? `${usedPct}%` : "—"}
          </strong>
        </div>
        <div className="sidebar-foot-row">
          <span>*arrs healthy</span>
          <strong>{totalCount > 0 ? `${healthyCount}/${totalCount}` : "—"}</strong>
        </div>
        {data?.last_runs?.[0] && (
          <div className="sidebar-foot-row">
            <span>Last run</span>
            <strong>{relativeTime(data.last_runs[0].triggered_at)}</strong>
          </div>
        )}
        <div className="sidebar-foot-divider" />
        <div className="sidebar-foot-row">
          <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
            <span className={`dot ${data ? "green" : "red"}`} />
            {data ? "Daemon up" : "Connecting…"}
          </span>
          <button
            onClick={handleThemeToggle}
            aria-label="Toggle dark/light mode"
            className="btn btn-ghost btn-sm"
            style={{ height: 22, padding: "0 5px" }}
          >
            {theme === "dark" ? <Sun size={13} /> : <Moon size={13} />}
          </button>
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
      {/* Desktop sidebar (hidden on small screens via CSS) */}
      <div style={{ display: "contents" }} className="sidebar-desktop">
        <Sidebar />
      </div>

      {/* Mobile: slide-in drawer + scrim */}
      {drawerOpen && (
        <>
          <div className="scrim" style={{ zIndex: 35 }} onClick={() => setDrawerOpen(false)} />
          <div style={{ position: "fixed", top: 0, left: 0, bottom: 0, width: 240, zIndex: 40 }}>
            <Sidebar onClose={() => setDrawerOpen(false)} />
          </div>
        </>
      )}

      {/* Main content */}
      <main className="main">
        {/* Mobile topbar */}
        <div
          className="topbar"
          style={{
            display: "none",
            position: "sticky",
            top: 0,
            zIndex: 20,
            paddingLeft: 10,
          }}
          id="mobile-topbar"
        >
          <button
            onClick={() => setDrawerOpen(true)}
            aria-label="Open menu"
            className="btn btn-ghost btn-sm"
            style={{ padding: "0 6px" }}
          >
            <Menu size={16} />
          </button>
          <Logo size={22} />
          <span style={{ fontWeight: 600, fontSize: 13, letterSpacing: "-0.01em" }}>Triagearr</span>
        </div>
        <style>{`
          @media (max-width: 768px) {
            .sidebar-desktop { display: none !important; }
            #mobile-topbar { display: flex !important; }
          }
        `}</style>
        <Outlet />
      </main>
    </div>
  );
}

export const Route = createRootRoute({ component: Layout });
