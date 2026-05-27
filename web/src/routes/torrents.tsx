import { createFileRoute, Outlet } from "@tanstack/react-router";

// Layout route for /torrents. The list lives in torrents.index.tsx and the
// per-torrent detail renders in the drawer over the list.
export const Route = createFileRoute("/torrents")({
  component: () => <Outlet />,
});
