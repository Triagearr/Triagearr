import { createFileRoute } from "@tanstack/react-router";
import { PollingSection } from "@/components/settings/PollingSection";

export const Route = createFileRoute("/settings/polling")({ component: PollingSection });
