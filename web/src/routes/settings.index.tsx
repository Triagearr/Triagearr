import { createFileRoute, redirect } from "@tanstack/react-router";

// Hitting /settings lands on the first section.
export const Route = createFileRoute("/settings/")({
  beforeLoad: () => {
    throw redirect({ to: "/settings/scoring" });
  },
});
