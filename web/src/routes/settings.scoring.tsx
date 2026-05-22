import { createFileRoute } from "@tanstack/react-router";
import { ScoringSection } from "@/components/settings/ScoringSection";

export const Route = createFileRoute("/settings/scoring")({ component: ScoringSection });
