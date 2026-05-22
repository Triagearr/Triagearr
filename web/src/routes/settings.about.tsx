import { createFileRoute } from "@tanstack/react-router";
import { useVersion } from "@/api/hooks";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/Card";

function AboutSection() {
  const version = useVersion();
  return (
    <Card>
      <CardHeader>
        <CardTitle>About</CardTitle>
      </CardHeader>
      <CardContent className="text-sm space-y-1">
        <div>
          version: <span className="font-mono">{version.data?.version ?? "unknown"}</span>
        </div>
        <div>
          commit: <span className="font-mono">{version.data?.commit ?? "unknown"}</span>
        </div>
        <div>
          built: <span className="font-mono">{version.data?.date ?? "unknown"}</span>
        </div>
        <div className="pt-2">
          <a
            className="text-primary underline"
            href="https://github.com/Triagearr/Triagearr"
            target="_blank"
            rel="noreferrer"
          >
            GitHub
          </a>
        </div>
      </CardContent>
    </Card>
  );
}

export const Route = createFileRoute("/settings/about")({ component: AboutSection });
