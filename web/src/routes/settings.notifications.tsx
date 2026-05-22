import { createFileRoute } from "@tanstack/react-router";
import { NotificationSection } from "@/components/settings/NotificationSection";

export const Route = createFileRoute("/settings/notifications")({
  component: NotificationSection,
});
