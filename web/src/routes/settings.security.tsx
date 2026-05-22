import { createFileRoute } from "@tanstack/react-router";
import { SecuritySection } from "@/components/SecuritySection";

export const Route = createFileRoute("/settings/security")({ component: SecuritySection });
