import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createRouter } from "@tanstack/react-router";

import "@/styles/globals.css";
import { applyTheme, resolveTheme } from "@/lib/theme";
import { routeTree } from "./routeTree.gen";

// Apply theme before React mounts to avoid flash of wrong colours.
applyTheme(resolveTheme());
import { LoginGate } from "@/components/LoginGate";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 30_000,
      // Pause polling when the tab is hidden so a forgotten dashboard tab
      // doesn't keep the daemon's writer pool warm for nothing.
      refetchIntervalInBackground: false,
    },
  },
});

const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  context: { queryClient },
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

const el = document.getElementById("root");
if (!el) throw new Error("missing #root");

createRoot(el).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <LoginGate>
        <RouterProvider router={router} />
      </LoginGate>
    </QueryClientProvider>
  </StrictMode>,
);
