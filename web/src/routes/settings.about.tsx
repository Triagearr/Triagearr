import { createFileRoute } from "@tanstack/react-router";
import { useVersion } from "@/api/hooks";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/Card";
import { m } from "@/paraglide/messages";

function AboutSection() {
  const version = useVersion();
  return (
    <Card>
      <CardHeader>
        <CardTitle>{m.settings_about_title()}</CardTitle>
      </CardHeader>
      <CardContent className="text-sm space-y-1">
        <div>
          {m.settings_about_version()} <span className="font-mono">{version.data?.version ?? m.settings_about_unknown()}</span>
        </div>
        <div>
          {m.settings_about_commit()} <span className="font-mono">{version.data?.commit ?? m.settings_about_unknown()}</span>
        </div>
        <div>
          {m.settings_about_built()} <span className="font-mono">{version.data?.date ?? m.settings_about_unknown()}</span>
        </div>
        <div className="pt-2">
          <a
            className="text-primary underline"
            href="https://github.com/Triagearr/Triagearr"
            target="_blank"
            rel="noreferrer"
          >
            {m.settings_about_github()}
          </a>
        </div>
      </CardContent>
    </Card>
  );
}

export const Route = createFileRoute("/settings/about")({ component: AboutSection });
