# CLAUDE.md — CF CLI Plugin Interface V3 RFC Proposal

## Project Overview

This repository contains an RFC proposal to modernize the Cloud Foundry CLI plugin interface. The RFC proposes replacing the current unmaintained plugin API with a minimal, stable contract where the CLI provides only authentication, session context, and API endpoints — plugins interact with CAPI V3 directly using their own clients.

**Tracking issue:** [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621)

## Repository Structure

| File | Purpose |
|------|---------|
| `rfc-draft-cli-plugin-interface-v3-detailed.md` | Full technical specification for the V3 plugin interface. Sections: Meta, Summary, Problem, Proposal, References. |
| `rfc-draft-plugin-transitional-migration.md` | Community-submission RFC for transitional migration — concise, follows CF RFC template format. |
| `rfc-draft-plugin-transitional-migration-detailed.md` | Full technical specification for transitional migration — code examples, field mappings, worked examples, companion package design. |
| `plugin-survey.md` | Survey of 18 actively maintained CF CLI plugins — how they use the plugin interface, workarounds, and migration patterns. Includes Rabobank case study. |
| `cli-plugin-interface-todo.md` | Detailed implementation plan for changes needed in the CF CLI codebase (`cloudfoundry/cli`). Organized by phases (1–6). |
| `TODO.md` | High-level work item tracker — research, decisions, stakeholder review, reference implementation, community process. |
| `plugin-repo-analysis.md` | Standalone analysis of plugin repository strengths, weaknesses, and ecosystem comparison (informs RFC D). |
| `draft-single-rfc-plugin-modernization.md` | Single unified RFC draft covering all 4 parts of plugin modernization (for CLI WG meeting 2026-03-25). |
| `draft-split-rfcs-plugin-modernization.md` | Split RFC drafts (A, B, C, D) covering all 4 parts as independent RFCs (for CLI WG meeting 2026-03-25). |
| `README.md` | Repository introduction and document links. |

## Terminology

The RFC uses **Host** (the CF CLI process) and **Guest** (the plugin process) to distinguish the two sides of the plugin interface. The host launches the guest, provides authentication and context, and manages the plugin lifecycle. The guest registers with the host, receives commands, and performs its work.

## Key Architectural Decisions

These decisions are final and documented in the RFC. Do not contradict them:

1. **CfClient() is a companion package**, not part of the core contract. The core contract (`PluginContext`) provides only serializable primitives (strings, bools). A separate `cli-plugin-helpers/cfclient` package wraps go-cfclient for Go guests.

2. **Channel abstraction** for host-guest communication. The `PluginChannel` interface (`Send`/`Receive`/`Open`/`Close`) hides the wire protocol. Two implementations: `GobTCPChannel` (legacy net/rpc backward compat) and `JsonRpcChannel` (new polyglot).

3. **JSON-RPC 2.0** is the message format for new-protocol guests. stdout/stderr remain available for guest user output. The protocol uses a separate TCP transport.

4. **Embedded `CF_PLUGIN_METADATA:` marker** for install-time metadata extraction. The host scans the guest binary/script for this marker without executing it. Absence of marker = legacy Go guest (falls back to exec + gob/net/rpc).

5. **`CliCommand`/`CliCommandWithoutTerminalOutput` are legacy-only**. Not carried forward in the new JSON-RPC contract. Guests use their own clients for all domain operations.

6. **Polyglot support is enabled by design**. The embedded marker + JSON-RPC approach allows guests in any language (Python, Perl, Java, etc.).

## Open Decisions

These items in TODO.md under "Decisions Still Needed" are unresolved. When working on them, update both the RFC and TODO.md:

- Additional endpoint methods (generic `Endpoint(name)` vs. specific endpoints)
- JSON-RPC method names, parameter schemas, and error codes
- `CF_PLUGIN_METADATA:` JSON schema formal definition
- Plugin lifecycle events in JSON-RPC
- Error handling and edge case guidance
- Connection info passing mechanism (env vars vs. other)
- Whether serialization format must be fixed to JSON

## Conventions

### Document Style
- Documents are Markdown with GitHub-flavored extensions (tables, task lists, fenced code blocks).
- The RFC follows CF community RFC conventions: `# Meta` header with Name, Start Date, Author, Status, RFC PR link, and Tracking Issue.
- Code examples use Go for the core contract and interface definitions. Python/Perl examples appear where polyglot support is relevant.
- Tables are used extensively for cross-plugin comparisons and format summaries.

### Document Audience Awareness
- The **Problem** section is read by all audiences (managers, TOC, CLI team, plugin developers). Keep it focused on quantifying risk and blast radius — tables, counts, severity assessments. No implementation-level detail (Go function signatures, module layouts, code examples, line counts).
- The **Proposal** section targets implementers (plugin developers, CLI team). Put package designs, function signatures, code examples, and migration instructions here.
- The **Summary** section is the executive overview — concise enough for managers and reviewers to assess scope and risk without reading the full document.
- Cross-link between sections rather than duplicating or misplacing content. Use anchor links (`[text](#heading-slug)`) so readers can navigate from high-level to detail.
- When adding new content, ask: "Who needs to read this?" and place it in the section that audience reads first.

### Cross-Document Consistency
- When updating the RFC, check whether `cli-plugin-interface-todo.md`, `plugin-survey.md`, and `TODO.md` need corresponding updates.
- Decisions should be recorded in TODO.md under "Decisions Made" with a reference to the RFC section.
- New research findings should be added to TODO.md under "Research & Analysis (Completed)" when done.

### Writing Style
- Detail increases by audience level, from minimal technical background to very technical:
  - **Summary** — accessible to managers, reviewers, and TOC members with minimal technical background. Marketing-style language is appropriate here to communicate value and urgency.
  - **Problem** — technical enough to quantify risk and scope for engineering managers and team leads. Tables, counts, severity assessments — not code.
  - **Proposal** — very technical, targeting plugin developers and CLI team engineers. Code examples, function signatures, implementation detail.
- **Define terms before use.** Never reference a term (e.g., "V2 domain methods", "Host", "Guest") before it has been defined. The glossary must precede any prose that uses its terms.
- Use RFC 2119 keywords (MUST, SHOULD, MAY) in the RFC document per convention.
- When referencing the current CLI codebase, use full file paths relative to the CLI repo root (e.g., `plugin/rpc/cli_rpc_server.go`).
- **Always include rationale.** Every architectural decision, design choice, or recommendation MUST include the reasoning behind it — the "why", not just the "what". Record rationale in both the RFC document (inline with the decision) and in TODO.md (under "Decisions Made"). A decision without rationale is incomplete.

## Related Repositories

These sibling repositories under `/Users/norm/Projects/CloudFoundry/` contain the actual source code referenced by the RFC:

- `cf-cli/` — The CF CLI source (`cloudfoundry/cli`). Key paths:
  - `plugin/plugin.go` — Current `Plugin`, `CliConnection`, `VersionType`, `PluginMetadata`, `Command`, `Usage` interfaces
  - `plugin/cli_connection.go` — Current RPC client (dials TCP per call via gob)
  - `plugin/rpc/cli_rpc_server.go` — Host-side RPC server (`CliRpcService`, `CliRpcCmd`, all config-derived methods)
  - `plugin/rpc/run_plugin.go` — Plugin launch (`exec.Command(path, port, args...)`)
  - `command/common/install_plugin_command.go` — Install flow
  - `command/plugin/shared/rpc.go` — `GetMetadata()` via `SendMetadata` arg
  - `actor/pluginaction/install.go` — `GetAndValidatePlugin`, `LibraryVersion` check

- `app-autoscaler-cli-plugin/` — App Autoscaler plugin (`cloudfoundry/app-autoscaler-cli-plugin`). Referenced for version workaround analysis (ldflags injection of build metadata in `Makefile`, dual-version printing in `main.go`).

## Migration Phases

The RFC defines a 4-phase migration timeline. The detailed implementation breakdown is in `cli-plugin-interface-todo.md` (Phases 1–6):

1. **Phase 1: Channel Abstraction and Embedded Metadata** (Q3 2026)
2. **Phase 2: JSON-RPC and Polyglot Support** (Q4 2026)
3. **Phase 3: Deprecation** (Q1 2027)
4. **Phase 4: Removal** (Q3 2027 or later)

## Workflow Notes

- Always commit with descriptive messages. The project uses conventional-style commits (no prefix tags, but the first line summarizes the change; body explains the "why").
- Push to `origin/main` after commits.
- The RFC was renamed from V2 to V3 to match CAPI versioning. All documents should reference V3.
