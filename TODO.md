# TODO — CLI Plugin Interface V3 RFC

## RFC Document

### Research & Analysis (Completed)

- [x] Survey actively maintained CF CLI plugins (19 active + 1 inactive = 20 plugins analyzed — see [plugin-survey.md](plugin-survey.md))
- [x] Build AST-based scanner (`cf-plugin-migrate`) to validate manual survey — V2 method detection, CliCommand tracing, internal import detection, API endpoint discovery
- [x] Run scanner across all 19 surveyed plugins, cross-validate with manual findings (discovered 4 missed V2 domain method usages: html5 GetOrg/GetSpace/GetServices, swisscom GetSpace)
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
- [x] Analyze host-side V3 rewrite POC ([cli#3741](https://github.com/cloudfoundry/cli/pull/3741)) — jcvrabo's proof-of-concept rewrites 9/10 domain methods in `cli_rpc_server.go` to use `v7action.Actor` + ccv3 instead of legacy commandregistry. ~800 lines changed, `GetApp` left on legacy path, 9 test cases removed without replacements, incomplete field mapping (empty GUIDs, missing quotas, hardcoded `IsAdmin: false`). Approaches are not mutually exclusive — host-side buys time, guest-side eliminates coupling. See [detailed RFC "Host-Side Alternative"](rfc-draft-plugin-transitional-migration-detailed.md#host-side-alternative-rewriting-cli_rpc_servergo-against-v3).

### Decisions Made

- [x] `CfClient()` placement → **Companion package** (`cli-plugin-helpers/cfclient`), not core contract. Core contract provides only serializable primitives. **Rationale:** The core contract (`PluginContext`) must be language-agnostic — serializable strings and bools that any JSON-RPC client can consume. A Go-specific `*client.Client` cannot be serialized across the wire. Placing it in a companion package keeps the core contract polyglot while giving Go plugins a convenient one-liner to get a configured go-cfclient. See RFC "CF Client Access" section.
- [x] Communication architecture → **Channel abstraction** (`Send`/`Receive`/`Open`/`Close`) with `GobTCPChannel` (legacy) and `JsonRpcChannel` (new polyglot). **Rationale:** The existing gob/net-rpc protocol cannot be changed without breaking all existing plugins. A channel abstraction lets the host support both legacy (gob) and new (JSON-RPC) protocols simultaneously, with protocol selection driven by the plugin's embedded metadata marker. This avoids a flag-day migration — existing plugins work unchanged, new plugins opt into JSON-RPC. See RFC "Communication Architecture" section.
- [x] Message format → **JSON-RPC 2.0** for new-protocol plugins. stdout/stderr reserved for plugin user output. **Rationale:** JSON-RPC 2.0 is a simple, well-specified standard with client libraries in every language, enabling polyglot plugins (Python, Perl, Java, etc.). It uses a separate TCP transport rather than stdout/stdin, leaving stdout/stderr available for plugin user-facing output — matching the existing behavior where plugins write directly to the terminal. Custom binary protocols or gRPC were rejected as unnecessarily complex for the small message surface (session context, lifecycle events).
- [x] Install-time metadata → **Embedded `CF_PLUGIN_METADATA:` marker** scanned from the binary/script. No execution needed. Legacy plugins detected by absence of marker. **Rationale:** The current install flow executes the plugin binary with a `SendMetadata` argument to retrieve metadata — this requires the binary to be runnable on the host platform, prevents cross-platform plugin repos, and is a security concern (arbitrary code execution at install time). Scanning for an embedded marker is safe, fast, and works for any language (compiled binaries, scripts with the marker in a comment, JARs with the marker in a resource). Absence of the marker tells the host this is a legacy Go plugin, enabling graceful fallback.
- [x] `CliCommand`/`CliCommandWithoutTerminalOutput` → **Legacy protocol only**. Not part of the new JSON-RPC contract. Plugins use their own clients for CAPI access. **Rationale:** These methods ask the host to execute arbitrary CLI commands on the plugin's behalf — creating tight coupling to CLI command names, output format, and behavior across versions. They exist because the original plugin interface provided no other way to access CAPI. With the new contract providing session credentials and endpoint URLs, plugins can call CAPI directly using their own HTTP clients. Carrying `CliCommand` forward would perpetuate the fragile parsing patterns and host-side complexity that this RFC aims to eliminate.

### Decisions Still Needed

- [ ] Decide: Which additional endpoints to include (UAA, Doppler, Routing API, CredHub) — or provide a generic `Endpoint(name string)` method
- [ ] Define JSON-RPC method names, parameter schemas, and standard error codes (e.g., `NOT_LOGGED_IN`, `TOKEN_EXPIRED`, `NO_TARGET`)
- [ ] Define the `CF_PLUGIN_METADATA:` JSON schema formally (with `schema_version` field for evolution)
- [ ] Define plugin lifecycle events in JSON-RPC (install, uninstall, upgrade notifications)
- [ ] Add error handling and edge case guidance (expired tokens, no target, plugin crashes mid-stream)
- [ ] Decide: How to pass connection info to new-protocol plugins (env vars `CF_PLUGIN_PORT`, `CF_PLUGIN_PROTOCOL` vs. other mechanism)
- [ ] Decide: Does the message serialization format need to be fixed to JSON? The channel abstraction could support alternative serialization formats (e.g., MessagePack, CBOR, Protobuf) alongside JSON-RPC — the `CF_PLUGIN_METADATA:` marker could declare the preferred format.
- [x] Decide: Should the Plugin SDK include interfaces/functions to cover the non-plugin CLI internal packages that 8 plugins import? **No — separate `cf-plugin-helpers` module.** The Plugin SDK covers only the plugin contract types (`plugin.Plugin`, `plugin.CliConnection`, `plugin/models/*`). Internal package replacements belong in `cf-plugin-helpers` as standalone packages with matching function signatures, enabling import-swap migration. **Rationale:** The Plugin SDK's purpose is build-time decoupling of the plugin contract — it must mirror the host's wire format exactly and stay in lockstep with the host's type definitions. Mixing in unrelated utility packages (terminal UI, tracing, config path helpers) would bloat the SDK, create false coupling between contract types and utility code, and force SDK version bumps for utility changes that have nothing to do with the wire protocol. The `cf-plugin-helpers` module already exists (it provides `CliConnection` test doubles) and is the natural home for plugin-side utilities that don't affect the host-guest contract. See [transitional RFC "Decoupling via cf-plugin-helpers"](rfc-draft-plugin-transitional-migration.md#decoupling-internal-imports-via-cf-plugin-helpers).
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
  - [x] Phase G: Polish — golden file tests, CLI flags, error messages
    - [x] Golden file tests: 4 fixtures (session_only_plugin, getapp_guid_only_plugin, ocf_scheduler_plugin, metric_registrar_plugin) with -update flag for regeneration
    - [x] CLI flags: Added `-h`/`--help`/`help` support for all subcommands, `-o` output flag for generate, proper `flag.FlagSet` parsing, usage examples
    - [x] Error messages: Config-not-found suggests running scan, unknown command shows usage
  - [x] Phase H: Scanner — detect CLI internal package imports (`code.cloudfoundry.org/cli/...` beyond `plugin` and `plugin/models`). Reports in human-readable summary and YAML `internal_imports` section. Implemented in `scanner/imports.go` with 20 known import paths (both old and v8 variants). 9 new tests.
  - [x] Phase H+: Scanner outputs replacement suggestions (`cf-plugin-helpers` import paths) for each detected internal import. 9 of 11 replacements covered. Remaining 2 are per-plugin issues:
    - `cf/terminal` Pattern A (multiapps): scanner detects and suggests `cfui` but notes "Pattern A may need additional work"
    - `cf/configuration` + `coreconfig` (cf-targets): scanner detects and notes "no drop-in replacement, see RFC for options"
- [x] Document token lifecycle pattern (`config.TokenProvider()` for long-running plugins) — see [transitional RFC token lifecycle](rfc-draft-plugin-transitional-migration.md#token-lifecycle)
- [x] Proof-of-concept: Analyze and walk through list-services migration (Tier 1: simple) — see [transitional RFC worked example](rfc-draft-plugin-transitional-migration.md#list-services-tier-1-simplest-domain-method-migration). Key finding: plugin is already 90% V3; demonstrates all three coupling patterns (V2 domain method, cf curl, CLI internal imports) in simplest form.
- [x] Proof-of-concept: Analyze and walk through OCF Scheduler migration (Tier 2: moderate) — see [transitional RFC worked example](rfc-draft-plugin-transitional-migration.md#worked-example-ocf-scheduler-plugin)
- [x] Proof-of-concept: Analyze and walk through metric-registrar migration (Tier 3: complex) — see [transitional RFC worked example](rfc-draft-plugin-transitional-migration.md#worked-example-metric-registrar-plugin-complex-migration)
- [x] Analyze Rabobank consumer plugins to verify whether full V2 reimplementation was necessary — see [transitional RFC consumer analysis](rfc-draft-plugin-transitional-migration.md#consumer-plugin-analysis-was-the-full-reimplementation-necessary)
- [x] Deep analysis: V2 app ports → V3 route destinations migration (internal routes, metric-registrar use case) — see [transitional RFC deep analysis](rfc-draft-plugin-transitional-migration.md#deep-analysis-v2-ports--v3-route-destinations). Key finding: V3 has no equivalent of V2 `ports` array for non-routable container ports. Migration requires internal routes + destinations (cross-component redesign with platform scraper).
- [ ] Package the V2→V3 translation code for host-side reuse. The generated V2Compat code populates `plugin_models.*` types from CAPI V3 responses — the same logic the CLI's `cli_rpc_server.go` uses command runner calls to accomplish via V2. Extracting this into a reusable package would let the CLI team replace their legacy command runner calls in `plugin/rpc/cli_rpc_server.go` with go-cfclient V3 calls, eliminating the host's V2 dependency for plugin support. Also useful for integration testing the generated wrappers. Note: the future plugin interface RFC (v9+) addresses long-term stability; this is a transitional bridge.

## cf-plugin-helpers Decoupling Packages

Standalone replacement packages with matching function signatures so plugins migrate by changing import paths only. Best-effort behavior — only signatures need to be exact matches. See [transitional RFC design](rfc-draft-plugin-transitional-migration.md#decoupling-internal-imports-via-cf-plugin-helpers).

- [x] `cfconfig` package — replaces `cf/configuration/confighelpers` (5 plugins). Two functions: `DefaultFilePath()`, `PluginRepoDir()`. Implemented with `$CF_HOME` fallback. 5 tests.
- [x] `cfformat` package — replaces `cf/formatters` (1 plugin). One function: `ByteSize(int64) string`. Implements B/K/M/G/T formatting. 7 test cases.
- [x] `cftrace` package — replaces `cf/trace` (6 plugins) and provides V3 call tracing. `Printer` interface + `NewLogger()` + `NewWriterPrinter()` + `NewTracingTransport()`. `NewTracingTransport` wraps `http.RoundTripper` to log HTTP request/response when `CF_TRACE` is enabled with `[PRIVATE DATA HIDDEN]` for auth headers. 10 tests.
- [x] `cfui` package — replaces `cf/terminal` Pattern B (5 plugins). `UI` interface (`Say`, `Warn`, `Failed`, `Ok`, `Table`), `TeePrinter`, color functions (`EntityNameColor`, `CommandColor`, `FailureColor`), `InitColorSupport()`. Uses `text/tabwriter` + ANSI escapes. 11 tests.
- [x] Update `cf-plugin-migrate scan` (Phase H+) to output replacement import paths when internal packages are detected. Scanner outputs `cf-plugin-helpers` paths in both summary and YAML.
- [x] Update `cf-plugin-migrate generate` to wire `cftrace.NewTracingTransport` into generated V2Compat constructor. CF_TRACE-aware tracing injected automatically via `config.HttpClient()`. Golden files regenerated.
- [x] Fix golden test `-update` flag to use `flag.Bool` instead of manual `os.Args` scan — `go test -update` now works.
- [x] Validate import-swap migration with app-autoscaler-cli-plugin. 4 import lines changed across 4 files, zero code changes, all tests pass. Requires import aliases (`trace "code.cloudfoundry.org/cf-plugin-helpers/cftrace"`, `confighelpers "code.cloudfoundry.org/cf-plugin-helpers/cfconfig"`) because the replacement package names differ from the originals. Validated against upstream commit `641a1a8` (pre-V3 changes). Also discovered and fixed `cfconfig.DefaultFilePath()` signature mismatch — original returns `(string, error)`, not `string`. `PluginRepoDir` is a `var` (function variable) checking `$CF_PLUGIN_HOME`, not a regular function checking `$CF_HOME`.

**Not covered by cf-plugin-helpers** (per-plugin remediation):
- `cf/terminal` Pattern A (multiapps — 16 files, full UI framework). Same three-option pattern as `cf/configuration`: (a) copy `cf/terminal` into plugin — but it pulls in `cf/trace`, `cf/i18n`, and internal types, not a clean extract, (b) keep existing import — works while frozen, (c) request CLI team extract into standalone module. CLI team is not bound to maintain compatibility.
- `cf/flags` (Swisscom appcloud — 1 struct field, use stdlib `flag`)
- `cf/configuration` + `coreconfig` (cf-targets — see below)

**CLI team engagement (optional):**
- [ ] Request CLI team consider providing a supported integration point for plugin config file access (`cf/configuration` + `coreconfig`). cf-targets-plugin reads and writes `~/.cf/config.json` directly to implement target switching. The config file format is undocumented and the CLI team is not bound to maintain compatibility. Three options exist: (a) copy the code into the plugin or `cf-plugin-helpers` — eliminates import but owns an undocumented format, (b) keep the existing import — same tight coupling as today, works while packages remain frozen, (c) CLI team provides a supported integration point (documented format, plugin RPC method, or supported package) — the only path to a stable contract. The choice is a risk tolerance decision for the plugin team. Option (c) is the best outcome but depends on CLI team willingness.
- `util/configv3` + `util/ui` (mysql-cli — transitive; eliminate by replacing one confirmation prompt)

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

- [x] Post RFC draft to cloudfoundry/community as a PR — [community#1452](https://github.com/cloudfoundry/community/pull/1452)
- [x] Mention @cloudfoundry/toc and relevant working groups — App Runtime Interfaces (CLI project)
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
- [x] Document coupling patterns in the transitional migration RFC (audience: managers/reviewers need to understand the blast radius of CLI internal changes) — see [transitional RFC "Plugins Import CLI Internal Packages"](rfc-draft-plugin-transitional-migration.md#plugins-import-cli-internal-packages)
- [x] Function-level audit of all CLI internal package usage across plugins — documented in [transitional RFC "Function-Level Complexity Assessment"](rfc-draft-plugin-transitional-migration.md#function-level-complexity-assessment). Key finding: **7 of 9 packages are trivial to replace** (a few functions each, not major integrations). `cf/i18n` and half of `cf/trace` are transitive dependencies of `cf/terminal` — removing terminal eliminates them.
- [ ] Validate replacement suggestions for each CLI internal package before adding to scanner:

  | Complexity | Package | Actual Functions Used | Replacement | Status |
  |---|---|---|---|---|
  | Trivial | `confighelpers` | `DefaultFilePath()`, `PluginRepoDir()` — 2 functions | ~5 lines stdlib (`$CF_HOME` → `$HOME/.cf` fallback) | **Ready** — can add to scanner |
  | Trivial | `cf/formatters` | `ByteSize()` — 1 call site (multiapps) | Inline or `dustin/go-humanize` | **Ready** — can add to scanner |
  | Trivial | `cf/flags` | `FlagContext` type — 1 struct field (Swisscom appcloud) | stdlib `flag` or `pflag` | **Ready** — can add to scanner |
  | Transitive | `cf/i18n` | `i18n.T` set to no-op passthrough — both plugins | Eliminated by removing `cf/terminal` | **Ready** — scanner should note transitive dependency |
  | Transitive | `cf/trace` (partial) | `NewWriterPrinter()` — passed to `terminal.NewUI()` | Eliminated by removing `cf/terminal` | **Ready** — scanner should note transitive dependency |
  | Small | `cf/trace` (actual) | `NewLogger()` — app-autoscaler HTTP tracing | `cftrace.NewLogger()` — same package also provides `NewTracingTransport` for V3 calls | **Ready** — CF_TRACE decision resolved |
  | Small | `util/ui` | `NewUI()` → `DisplayBoolPrompt()`, `DisplayText()` — 1 prompt (mysql-cli) | `fmt.Print` + `bufio.Scanner` (~10 lines) | **Ready** — can add to scanner |
  | Medium | `cf/terminal` Pattern B | `NewUI`, `NewTeePrinter`, `InitColorSupport` — 5 plugins, UI bootstrap only | `text/tabwriter` for tables, optional `fatih/color` | **Ready** — can add to scanner |
  | Medium-Large | `cf/terminal` Pattern A | Full UI framework — multiapps only, 16 files, 30+ `EntityNameColor` calls | `text/tabwriter` + `fatih/color` or `charmbracelet/lipgloss` | Per-plugin remediation, not scanner suggestion |
  | Hard | `cf/configuration` + `coreconfig` | `NewDiskPersistor()`, `NewData()`, `.JSONMarshalV3()` — cf-targets only | No clean drop-in; undocumented config file format | Needs design-level resolution |
  | Hard | `util/configv3` | `&configv3.Config{}` empty struct passed to `ui.NewUI()` — mysql-cli only | Eliminated when `util/ui` is replaced | **Ready** — transitive dependency of `util/ui` |

## CF_TRACE Behavioral Regression

- [x] Research: Does go-cfclient v3 have built-in tracing/debug support? **No.** No `CF_TRACE`, no debug logging, no request/response dumping.
- [x] Research: Can tracing be injected? **Yes.** `config.HttpClient(*http.Client)` accepts a custom HTTP client. A tracing `http.RoundTripper` can wrap the base transport. Transport chain: `retryableAuthTransport` → `oauth2.Transport` → user's Transport.
- [x] Research: TLS caveat identified. go-cfclient's `getHTTPTransport()` only recognizes `*http.Transport` or `*oauth2.Transport` for `skipTLSValidation`. Custom `RoundTripper` types won't get TLS config applied automatically — the tracing transport must wrap a properly configured `*http.Transport`.
- [x] Research: CF_TRACE output format. CLI uses REQUEST/RESPONSE blocks with timestamps, sorted headers, `[PRIVATE DATA HIDDEN]` for auth headers, JSON body pretty-printing/sanitization.
- [x] Decide: **Yes — `cftrace.NewTracingTransport()` in `cf-plugin-helpers/cftrace`.** Without this, developers debugging issues with the transitional generated code cannot see what HTTP requests the V2Compat wrapper makes to CAPI V3, what responses it receives, or why a V3 call produces different results than the V2 call it replaced. Before migration, `CF_TRACE=true` shows the host's V2 API calls; after migration, the same operations happen guest-side via go-cfclient and are invisible. Tracing is essential during the migration period when developers are actively verifying V2-to-V3 behavioral equivalence. Generated V2Compat wrapper injects it automatically via `config.HttpClient()`. Output is best-effort CF CLI format (REQUEST/RESPONSE blocks, auth header sanitization). Lives in `cftrace` package alongside `Printer`/`NewLogger`/`NewWriterPrinter`, so it serves both the legacy `cf/trace` replacement role and the V3 call tracing role. See [transitional RFC cftrace design](rfc-draft-plugin-transitional-migration.md#decoupling-internal-imports-via-cf-plugin-helpers).

## Future RFCs (Out of Scope)

- [ ] Polyglot plugin support (gRPC-based plugin model)
- [ ] Official CAPI OpenAPI specification — no machine-readable spec exists ([cloud_controller_ng#2192](https://github.com/cloudfoundry/cloud_controller_ng/issues/2192), open since 2021). Community [capi-openapi-spec](https://github.com/cloudfoundry-community/capi-openapi-spec) under `cloudfoundry-community` parses HTML docs → OpenAPI 3.0.0. An official spec would enable auto-generated CAPI clients in any language, strengthening the polyglot plugin story.
- [ ] GitHub-style plugin distribution and trust model
- [ ] CLI adoption of go-cfclient internally
- [ ] Standard option parsing framework for plugins
