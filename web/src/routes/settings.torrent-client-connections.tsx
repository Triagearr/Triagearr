import { createFileRoute } from "@tanstack/react-router";
import { TorrentClientConnectionsSection } from "@/components/settings/TorrentClientConnectionsSection";

export const Route = createFileRoute("/settings/torrent-client-connections")({
  component: TorrentClientConnectionsSection,
});
