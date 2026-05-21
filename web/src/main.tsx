import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider, createRouter } from "@tanstack/react-router";

import "@/styles/globals.css";
import { routeTree } from "./routeTree.gen";
import { ApiKeyGate } from "@/components/ApiKeyGate";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 5_000 } },
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
      <ApiKeyGate>
        <RouterProvider router={router} />
      </ApiKeyGate>
    </QueryClientProvider>
  </StrictMode>,
);
