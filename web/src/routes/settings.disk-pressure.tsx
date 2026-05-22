import { createFileRoute } from "@tanstack/react-router";
import { DiskPressureSection } from "@/components/settings/DiskPressureSection";

export const Route = createFileRoute("/settings/disk-pressure")({ component: DiskPressureSection });
