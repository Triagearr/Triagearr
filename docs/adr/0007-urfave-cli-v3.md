# ADR-0007: Use urfave/cli/v3 instead of spf13/cobra

## Status

Accepted — 2026-05-17

## Context

Two dominant CLI frameworks for Go:

- `spf13/cobra` v1.10.2 (Dec 2025): the de-facto standard, used by kubectl, gh, helm, hugo, k3s.
- `urfave/cli/v3` v3.9.0 (May 2026): an older project, recently revamped to v3 with modern API.

Triagearr's CLI surface is small:

```
triagearr serve          # run daemon
triagearr inspect ...    # subcommands for inspecting state
triagearr score ...      # scoring utilities
triagearr migrate ...    # DB migration helpers
triagearr version
triagearr health         # used by Docker HEALTHCHECK
```

~6 top-level commands, max one level of subcommands. No need for Cobra's deep-tree machinery.

## Comparison

| Dimension | Cobra | urfave/cli v3 |
|---|---|---|
| Last release | 2025-12-04 (~5 months) | 2026-05-12 (this week) |
| Release cadence | ~1/year | ~1/month |
| API style | Struct-heavy, verbose | Functional options, modern |
| Default Viper coupling | Yes (idiomatic combo) | None |
| Sub-routing capability | Excellent (deep trees) | Good (1-2 levels) |
| Doc generation | Built-in (man pages, markdown) | Built-in but less polished |
| Binary size impact | ~1.5 MB | ~0.4 MB |
| Transitive deps | `pflag`, `cobra` itself | minimal |
| Best fit | Tools with 20+ subcommands | Daemons with handful of commands |

For our exact shape, urfave/cli v3 is genuinely better fit. The Cobra+Viper "idiomatic combo" is unnecessary because we use koanf for config instead of viper, breaking the combo benefit.

## Decision

Use `github.com/urfave/cli/v3` v3.9.x.

API sample to anchor expectations:

```go
app := &cli.Command{
    Name: "triagearr",
    Usage: "Disk-pressure-aware media reaper",
    Commands: []*cli.Command{
        {
            Name:   "serve",
            Action: serveAction,
            Flags: []cli.Flag{
                &cli.StringFlag{Name: "config", Value: "/config/config.yml"},
            },
        },
        {Name: "inspect", Subcommands: inspectSubcommands},
        {Name: "version", Action: versionAction},
    },
}
app.Run(ctx, os.Args)
```

## Consequences

**Easier:**
- Smaller, more recent dep
- Functional options API plays well with idiomatic Go
- Less ceremony for simple commands
- No tempting Viper coupling

**Harder:**
- Less name recognition for new contributors ("Cobra" is the default mental model in Go land)
- If we ever grow to kubectl-scale subcommand depth, we'd want to migrate to Cobra. Not a foreseen scenario.

**Traded away:**
- Cobra's mature man-page and shell-completion generation. urfave/cli has equivalents but less polished.

## Re-evaluation triggers

- If urfave/cli's release cadence slows substantially (>6 months gap), revisit
- If the CLI surface grows past ~15 commands with >2 levels deep

## References

- Cobra: https://github.com/spf13/cobra
- urfave/cli: https://github.com/urfave/cli
- API v3 docs: https://cli.urfave.org/v3/
