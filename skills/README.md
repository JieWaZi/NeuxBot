# NeuxBot maintained skills

This directory is the source of truth for skills shipped with NeuxBot, separate from repository development skills in `.agents/skills/`.

The system `ally` skill is the single entry for application capabilities. Its short English `SKILL.md` routes agents to live `allycli capabilities`, `describe`, and the relevant guide. Add future application capabilities to the shared operation catalog and this skill's references instead of creating a separate built-in skill or MCP server.

`catalog.json` marks this package with `system: true`. Core materializes the current bundled resources into a digest-verified system directory for each managed execution. It does not create an editable skill package or partner enablement relation. The reserved name cannot be replaced through ordinary skill editing. Ordinary user skills and third-party MCP connectors retain their independent mechanisms.

After changing resources, run `pnpm skills:generate`, `pnpm skills:check`, and the relevant backend checks. The generator includes complete resource directories, binary files, and executable bits; it rejects symbolic links and special files. `server/internal/skills/builtin/bundle.generated.json` must only be generated. Server development and Core/sidecar builds regenerate it; `pnpm check` verifies it is current. Direct Go commands use the checked-in bundle, so regenerate it first.

CLI syntax and authorization come from `server/internal/agentcapability/contract`; workflow SDK and dynamic result schemas come from live commands. Guides teach decisions and verification, without duplicating those contracts.
