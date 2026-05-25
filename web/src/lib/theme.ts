export type Theme = "light" | "dark";

export function getStoredTheme(): Theme | null {
  try {
    const v = localStorage.getItem("theme");
    return v === "light" || v === "dark" ? v : null;
  } catch {
    return null;
  }
}

export function resolveTheme(): Theme {
  const stored = getStoredTheme();
  if (stored) return stored;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function applyTheme(theme: Theme) {
  document.documentElement.setAttribute("data-theme", theme);
  try {
    localStorage.setItem("theme", theme);
  } catch {
    /* noop */
  }
}

export function toggleTheme(): Theme {
  const current = document.documentElement.getAttribute("data-theme");
  const next: Theme = current === "dark" ? "light" : "dark";
  applyTheme(next);
  return next;
}
