import { createFileRoute } from "@tanstack/react-router";
import { ArrConnectionsSection } from "@/components/settings/ArrConnectionsSection";

export const Route = createFileRoute("/settings/arr-connections")({
  component: ArrConnectionsSection,
});
