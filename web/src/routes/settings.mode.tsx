import { createFileRoute } from "@tanstack/react-router";
import { ModeSection } from "@/components/settings/ModeSection";

export const Route = createFileRoute("/settings/mode")({ component: ModeSection });
