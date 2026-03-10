# TODO — CLI Plugin Interface V3 RFC

## RFC Document

### Research & Analysis (Completed)

- [x] Survey actively maintained CF CLI plugins (18 plugins analyzed — see [plugin-survey.md](plugin-survey.md))
- [x] Document how plugins interact with CF outside the plugin interface (go-cfclient, cf curl, direct HTTP, exec bypass, file I/O)
- [x] Analyze CF CLI plugin internals (`plugin/`, `plugin/models/`, `plugin/rpc/`) — see [cli-plugin-interface-todo.md](cli-plugin-interface-todo.md)
- [x] Analyze plugin help system integration (`command/common/help_command.go`, `GetMetadata().Commands`)
- [x] Draft the list of Core Plugin API methods (Session, Endpoint, Context)
- [x] Define concrete Go interface types (`PluginContext`, `OrgContext`, `SpaceContext`, `PluginVersion`, `Command`, `Usage`, `FlagDefinition`)
- [x] Draft migration timeline (Phases 1–4 in cli-plugin-interface-todo.md)
- [x] Add help system findings and proposals to RFC
- [x] Analyze `VersionType` limitations (Build misnomer, no prerelease/build metadata, plugin workarounds)
- [x] Analyze `Options map[string]string` limitations (unordered, no long/short pairing, no defaults/grouping, `ConvertPluginToCommandInfo` processing)
- [x] Review [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines) — standard help sections (NAME, USAGE/[docopt](http://docopt.org/), WARNING, EXAMPLE, TIP, ALIAS, OPTIONS, SEE ALSO)
- [x] Review [CF CLI Style Guide](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Style-Guide) — command naming, fail-fast validation, output formatting, color, flag design
- [x] Review [CLI Product-Specific Style Guide](https://github.com/cloudfoundry/cli/wiki/CLI-Product-Specific-Style-Guide) — error patterns, TIPs, confirmation prompts, idempotency
- [x] Review [Version Switching Guide](https://github.com/cloudfoundry/cli/wiki/Version-Switching-Guide) — interface evolution considerations
- [x] Add version and flag metadata findings to RFC, CLI TODO, and plugin survey
- [x] Add wiki guide references to RFC, CLI TODO, and plugin survey
- [x] Define interface evolution strategy (backward-compatible structs, additive RPC, capability discovery, deprecation signaling)
- [x] Add `Warning`, `Tip` fields to `Command` struct per Help Guidelines
- [x] Add `Group` field to `FlagDefinition` for organized flag display
- [x] Rename `PluginVersion.Build` → `Patch` for SemVer correctness; add `PreRelease`, `BuildMeta`, `String()`
- [x] Research `CF_PLUGIN_METADATA:` marker survivability in self-extracting and compressed executables (UPX, makeself, AppImage, NSIS, 7-Zip SFX, etc.) — character safety confirmed for JSON in all formats

### Decisions Made

- [x] `CfClient()` placement → **Companion package** (`cli-plugin-helpers/cfclient`), not core contract. Core contract provides only serializable primitives. See RFC "CF Client Access" section.
- [x] Communication architecture → **Channel abstraction** (`Send`/`Receive`/`Open`/`Close`) with `GobTCPChannel` (legacy) and `JsonRpcChannel` (new polyglot). See RFC "Communication Architecture" section.
- [x] Message format → **JSON-RPC 2.0** for new-protocol plugins. stdout/stderr reserved for plugin user output.
- [x] Install-time metadata → **Embedded `CF_PLUGIN_METADATA:` marker** scanned from the binary/script. No execution needed. Legacy plugins detected by absence of marker.
- [x] `CliCommand`/`CliCommandWithoutTerminalOutput` → **Legacy protocol only**. Not part of the new JSON-RPC contract. Plugins use their own clients for CAPI access.

### Decisions Still Needed

- [ ] Decide: Which additional endpoints to include (UAA, Doppler, Routing API, CredHub) — or provide a generic `Endpoint(name string)` method
- [ ] Define JSON-RPC method names, parameter schemas, and standard error codes (e.g., `NOT_LOGGED_IN`, `TOKEN_EXPIRED`, `NO_TARGET`)
- [ ] Define the `CF_PLUGIN_METADATA:` JSON schema formally (with `schema_version` field for evolution)
- [ ] Define plugin lifecycle events in JSON-RPC (install, uninstall, upgrade notifications)
- [ ] Add error handling and edge case guidance (expired tokens, no target, plugin crashes mid-stream)
- [ ] Decide: How to pass connection info to new-protocol plugins (env vars `CF_PLUGIN_PORT`, `CF_PLUGIN_PROTOCOL` vs. other mechanism)
- [ ] Decide: Does the message serialization format need to be fixed to JSON? The channel abstraction could support alternative serialization formats (e.g., MessagePack, CBOR, Protobuf) alongside JSON-RPC — the `CF_PLUGIN_METADATA:` marker could declare the preferred format.
- [x] Discuss Rabobank transitional wrapper caveats → V2-to-V3 data shape differences (IsAdmin, single process, single buildpack, missing stats) resolved by generated wrapper approach. Implementation bugs (token prefix, SSL, user agent) documented. See [transitional RFC](rfc-draft-plugin-transitional-migration.md#lessons-from-the-rabobank-implementation).

### Stakeholder Review

- [ ] Review RFC draft with CLI maintainers (@a-b, @gururajsh, @anujc25, @moleske)
- [ ] Incorporate feedback from @beyhan and @silvestre on minimal API surface
- [ ] Incorporate feedback from @s-yonkov-yonkov (MTA plugin) on backward compatibility
- [ ] Incorporate feedback from @jcvrabo on go-cfclient integration and plugin repo management
- [ ] Incorporate feedback from @parttimenerd (cf-java-plugin) on dependency updates
- [ ] Review migration timeline (Phases 1–4) for feasibility with CLI team

## Transitional Migration (Phase 0)

- [x] Document go-cfclient/v3 minimum alpha version and CF API version floor — see [transitional RFC version guidance](rfc-draft-plugin-transitional-migration.md#go-cfclient-v3-version-guidance)
- [x] Define `cf-plugin-migrate.yml` YAML schema for generated V2 compatibility wrappers — see [transitional RFC YAML schema](rfc-draft-plugin-transitional-migration.md#yaml-schema-cf-plugin-migrateyml)
- [x] Define complete V2→V3 field mapping reference for all plugin models — see [transitional RFC field mapping](rfc-draft-plugin-transitional-migration.md#complete-v2v3-field-mapping-reference)
- [x] Document V2 plugin model struct reference — see [transitional RFC model reference](rfc-draft-plugin-transitional-migration.md#v2-plugin-model-struct-reference)
- [x] Implement `cf-plugin-migrate scan` (go/ast-based audit → YAML config) — see [transitional RFC scan design](rfc-draft-plugin-transitional-migration.md#automated-audit-cf-plugin-migrate-scan), [scanner test results](cf-plugin-migrate/SCANNER_TEST_RESULTS.md)
- [x] Implement `cf-plugin-migrate generate` (YAML config → Go source output) — see [design doc phases](cf-plugin-migrate-design.md#generate-implementation-phases)
  - [x] Phase A: Config parsing, group resolution, generator skeleton
  - [x] Phase B: Session pass-through + V2Compat struct — tested with cf-targets-plugin (zero-change migration: drop in generated file, `make build`, `cf install-plugin`, `cf targets` works)
  - [x] Phase C: Simple domain methods (GetOrgs, GetSpaces)
  - [x] Phase D: Medium domain methods (GetService, GetServices, GetOrg, GetSpace, GetOrgUsers, GetSpaceUsers)
  - [x] Phase E: Complex domain methods (GetApps, GetApp — dependency chains, per-item calls) — tested with OCF Scheduler against live CAPI V3 (v3.180.0): `cf create-job` resolved app via V3 `Applications.Single`
  - [x] Phase F: Scanner enhancement — detect all `CliCommand`/`CliCommandWithoutTerminalOutput` calls (command + args extraction), `cf curl` deep analysis (endpoint URL extraction, JSON unmarshal tracing, field access, V2→V3 endpoint mapping for 20 known endpoints). Validated against test_rpc_server_example, mysql-cli-plugin (14 calls), ocf-scheduler (0 calls).
  - [ ] Phase G: Polish — golden file tests, CLI flags, error messages
- [ ] Document token lifecycle pattern (`config.TokenProvider()` for long-running plugins)
- [ ] Proof-of-concept: Analyze and walk through list-services migration (Tier 1: simple)
- [x] Proof-of-concept: Analyze and walk through OCF Scheduler migration (Tier 2: moderate) — see [transitional RFC worked example](rfc-draft-plugin-transitional-migration.md#worked-example-ocf-scheduler-plugin)
- [x] Proof-of-concept: Analyze and walk through metric-registrar migration (Tier 3: complex) — see [transitional RFC worked example](rfc-draft-plugin-transitional-migration.md#worked-example-metric-registrar-plugin-complex-migration)
- [x] Analyze Rabobank consumer plugins to verify whether full V2 reimplementation was necessary — see [transitional RFC consumer analysis](rfc-draft-plugin-transitional-migration.md#consumer-plugin-analysis-was-the-full-reimplementation-necessary)
- [ ] Deep analysis: V2 app ports → V3 route destinations migration (internal routes, metric-registrar use case)

## Reference Implementation

- [ ] Create a standalone Go module for the new plugin interface
- [ ] Implement `PluginContext` interface in the CLI
- [ ] Build a reference plugin demonstrating the new pattern
- [ ] Migrate ocf-scheduler-cf-plugin as a real-world migration example
- [ ] Document migration steps from legacy interface to new interface

## Plugin Repository

- [ ] Draft plugin repository deprecation policy
- [ ] Define metadata schema for plugin compatibility (min CLI version, min CAPI version)
- [ ] Propose plugin repository format changes (version compatibility, deprecation attributes)

## Community Process

- [ ] Post RFC draft to cloudfoundry/community as a PR
- [ ] Mention @cloudfoundry/toc and relevant working groups
- [ ] Present at CF community call
- [ ] Collect feedback during public discussion period
- [ ] Request Final Comment Period (FCP)

## Plugin Host-Code Coupling Analysis

- [x] Audit all 18 surveyed plugins for direct imports of CF CLI host-side packages (`code.cloudfoundry.org/cli/...` beyond `plugin` and `plugin/models`). All audits performed against upstream/pre-V3 branches — not local work branches.

  **Result: 10 clean, 8 coupled.**

  Coupled plugins (production imports beyond `plugin`/`plugin/models`):
  - [x] cf-targets-plugin — `cf/configuration`, `cf/configuration/confighelpers`, `cf/configuration/coreconfig` (config file read/write)
  - [x] App Autoscaler — `cf/trace`, `cf/configuration/confighelpers` (tracing + config path)
  - [x] cf-app-autoscaler (v8 fork) — `cli/v8/cf/trace`, `cli/v8/cf/configuration/confighelpers` (same pattern, v8 import paths)
  - [x] MultiApps / MTA — `cli/v8/cf/terminal`, `cli/v8/cf/formatters`, `cli/v8/cf/i18n`, `cli/v8/cf/trace` (14+ production files — heaviest coupling)
  - [x] mysql-cli-plugin — `cf/configuration/confighelpers`, `util/configv3`, `util/ui` (deepest internal coupling)
  - [x] cf-java-plugin — `cf/terminal`, `cf/trace` (UI + tracing)
  - [x] Swisscom appcloud — `cf/flags`, `cf/terminal`, `cf/trace` (flags + UI + tracing)
  - [x] html5-apps-repo — `cf/terminal`, `cf/i18n` (UI + i18n; still uses old `github.com/cloudfoundry/cli` import path)
  - [x] list-services — `cf/terminal`, `cf/trace` (UI + tracing; pre-modules, no go.mod)

  Clean plugins (only import `plugin` and/or `plugin/models`):
  - [x] OCF Scheduler, Rabobank cf-plugins, upgrade-all-services, stack-auditor, log-cache-cli, DefaultEnv, metric-registrar, service-instance-logs, spring-cloud-services, cf-lookup-route

  **Coupling patterns by prevalence:**

  | Internal Package | Plugins | Purpose |
  |---|---|---|
  | `cf/terminal` | 6 | Colored/formatted output |
  | `cf/trace` | 6 | HTTP request tracing |
  | `cf/configuration/confighelpers` | 4 | Config file path discovery |
  | `cf/i18n` | 2 | Internationalization |
  | `cf/formatters` | 1 | Byte/size formatting |
  | `cf/flags` | 1 | Flag parsing |
  | `cf/configuration` + `coreconfig` | 1 | Direct config file read/write |
  | `util/configv3` | 1 | V3 config layer |
  | `util/ui` | 1 | UI rendering |

- [x] Analyze whether coupled internal packages have changed since plugins pinned their CLI dependency.

  **Result: The `cf/` packages are effectively frozen.**

  | Package | Commits Since 2020 | Exported API Changes | Breaking? |
  |---|---|---|---|
  | `cf/configuration/confighelpers` | **0** | None | No |
  | `cf/trace` | 4 | None (test infra only) | No |
  | `cf/terminal` | 7 | None (test/cosmetic only) | No |
  | `cf/formatters` | 1 | None (test only) | No |
  | `cf/i18n` | 2 | None (test/internal) | No |
  | `cf/flags` | 4 | None (test/cosmetic) | No |
  | `cf/configuration/coreconfig` | 7 | Additive (new fields, semver v4 dep) | Potentially |
  | `util/configv3` | **24** | Structural (K8s support, new embedded interface) | **Yes** |
  | `util/ui` | 19 | Additive (new methods only) | No |

  **Key insight:** The coupling hasn't broken plugins *yet* because the CLI team hasn't touched these packages. But this is luck, not design — any refactoring of `cf/terminal` or `cf/configuration` would break 8 plugins with no warning. The `util/configv3` package (mysql-cli-plugin only) has already diverged structurally.

  **Module path migration** from `code.cloudfoundry.org/cli` to `code.cloudfoundry.org/cli/v8` is an additional breaking change for plugins pinned to `v7.1.0+incompatible` (mysql, list-services, html5-apps-repo).

- [ ] Create GitHub issues for plugins with host-code coupling
- [ ] Create Jira tickets for tracking host-code coupling remediation
- [ ] Document coupling patterns in the transitional migration RFC (audience: managers/reviewers need to understand the blast radius of CLI internal changes)

## Future RFCs (Out of Scope)

- [ ] Polyglot plugin support (gRPC-based plugin model)
- [ ] Official CAPI OpenAPI specification — no machine-readable spec exists ([cloud_controller_ng#2192](https://github.com/cloudfoundry/cloud_controller_ng/issues/2192), open since 2021). Community [capi-openapi-spec](https://github.com/cloudfoundry-community/capi-openapi-spec) under `cloudfoundry-community` parses HTML docs → OpenAPI 3.0.0. An official spec would enable auto-generated CAPI clients in any language, strengthening the polyglot plugin story.
- [ ] GitHub-style plugin distribution and trust model
- [ ] CLI adoption of go-cfclient internally
- [ ] Standard option parsing framework for plugins
