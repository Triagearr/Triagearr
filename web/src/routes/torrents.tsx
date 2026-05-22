import { createFileRoute, Outlet } from "@tanstack/react-router";

// Layout route for /torrents. The list lives in torrents.index.tsx and the
// detail in torrents.$hash.tsx; this just renders whichever child is active.
export const Route = createFileRoute("/torrents")({
  component: () => <Outlet />,
});
