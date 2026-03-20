# Meta
[meta]: #meta
- Name: CLI Plugin Transitional Migration Guide
- Start Date: 2026-03-01
- Author(s): @norman-abramovitz
- Status: Draft
- Tracking Issue: [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621)

## Summary

**Key terms used throughout this document:**
- **Host** — the CF CLI process that launches and manages plugins
- **Guest** — the plugin process launched by the host
- **CAPI** — Cloud Controller API, the CF platform's REST API. CAPI V2 is reaching end of life; CAPI V3 is the current version.
- **V2 domain methods** — the 10 methods on the plugin interface that return CAPI V2-shaped data (e.g., `GetApp`, `GetApps`, `GetService` — [full list in Technical Reference](#v2-plugin-model-struct-reference))
- **Context methods** — the 11 methods that return session and authentication data (e.g., `AccessToken`, `ApiEndpoint`, `GetCurrentOrg` — [full list in Technical Reference](#context-models-pass-through--no-wrappers-needed))
- **go-cfclient** — the official Go client library for CAPI V3

The CF CLI plugin interface depends on CAPI V2, which is reaching end of life. When V2 endpoints are removed, plugins that rely on the host's V2 domain methods will break — affecting at least 18 actively maintained plugins across the Cloud Foundry ecosystem.

This document proposes a **transitional migration** that plugin teams can adopt **today**, without waiting for CLI team changes or a new plugin interface. A companion tool (`cf-plugin-migrate`) scans a plugin's source code, identifies exactly what V2 functionality it uses, and generates drop-in replacement code backed by CAPI V3.

1. **Immediate risk reduction.** Plugins eliminate their V2 dependency before the deprecation deadline. Session-only plugins require zero code changes; plugins using V2 domain methods require one new dependency and minimal code changes.

2. **No cross-team coordination required.** The migration runs entirely on the guest side — it works with any existing CF CLI version (v7, v8, v9). Plugin teams migrate at their own pace without blocking on CLI releases.

3. **Unblocks CLI simplification.** By moving data operations to the guest side, the CLI team is freed to remove legacy V2 support code on its own timeline — reducing maintenance burden and eliminating a class of RPC-related bugs.

This approach is validated by the [Rabobank `cf-plugins`](https://github.com/rabobank/cf-plugins) library, which has been in production use since 2025 (see [plugin survey case study](plugin-survey.md#case-study-rabobank-guest-side-transitional-wrapper)).

## Problem

### CAPI V2 Is Reaching End of Life

Plugins that depend on V2-shaped data from the host's domain methods (`GetApp`, `GetApps`, `GetService`, etc.) will stop working when V2 endpoints are removed. The current plugin interface returns `plugin_models.*` types that mirror V2 response structures — these types cannot represent V3 concepts like multiple process types, sidecars, rolling deployments, service credential bindings, or metadata labels.

### The Host Carries Legacy Code for Plugin Support

The CF CLI host currently implements 10 V2 domain methods on behalf of plugins via RPC. Each method involves:
- An RPC handler in `plugin/rpc/cli_rpc_server.go`
- V2-shaped response types in `plugin/models/`
- Config-derived accessors and state management

This code exists solely to serve the guest-side plugin interface. Once plugins fetch their own data via CAPI V3, the host can remove this entire subsystem — reducing maintenance burden, simplifying the CLI codebase, and eliminating a class of RPC-related bugs.

### CliCommand Is a Fragile Escape Hatch

Many plugins use `CliCommand` or `CliCommandWithoutTerminalOutput` to run arbitrary CF CLI commands, including `cf curl` for direct CAPI access. These calls:
- Parse CLI text output, which is fragile across CLI versions
- Cannot be statically analyzed by the host for deprecation planning
- Mix workflow orchestration (e.g., `push`, `bind-service`) with data access (e.g., `curl /v2/apps`)

A scan of 18 actively maintained plugins found `CliCommand` usage across 14 plugins, with patterns ranging from simple `cf apps` to complex `cf curl` with JSON parsing and pagination.

### Plugins Import CLI Internal Packages

Beyond the intended public interface (`plugin/` and `plugin/models/`), 8 of 18 surveyed plugins import internal CLI packages — creating a build-time dependency on code the CLI team never intended to expose.

An audit of all 18 plugins (performed against upstream default branches prior to any V3 migration work — noting that Rabobank's migration began in 2025) found two dominant coupling patterns:

| Internal Package | Plugins | Purpose |
|---|---|---|
| `cf/terminal` | 6 | Colored/formatted terminal output |
| `cf/trace` | 6 | HTTP request tracing/debug logging |
| `cf/configuration/confighelpers` | 4 | Config file path discovery |
| `cf/i18n` | 2 | Internationalization |
| Other (`cf/formatters`, `cf/flags`, `util/configv3`, `util/ui`) | 1 each | Formatting, flag parsing, config access, UI rendering |

**Coupled plugins:**

| Plugin | Internal Packages | Severity |
|---|---|---|
| MultiApps / MTA | `cf/terminal`, `cf/formatters`, `cf/i18n`, `cf/trace` | High — 14+ production files |
| mysql-cli-plugin | `cf/configuration/confighelpers`, `util/configv3`, `util/ui` | High — deepest internal coupling |
| App Autoscaler | `cf/trace`, `cf/configuration/confighelpers` | Medium |
| cf-targets-plugin | `cf/configuration`, `cf/configuration/confighelpers`, `cf/configuration/coreconfig` | Medium — config file read/write |
| Swisscom appcloud | `cf/flags`, `cf/terminal`, `cf/trace` | Medium |
| cf-java-plugin | `cf/terminal`, `cf/trace` | Low |
| html5-apps-repo | `cf/terminal`, `cf/i18n` | Low |
| list-services | `cf/terminal`, `cf/trace` | Low |

**These packages are currently frozen — but that's luck, not design.** Analysis of the CF CLI git history shows zero exported API changes in `cf/configuration/confighelpers` since 2020, and only test infrastructure updates (ginkgo v2) in `cf/terminal`, `cf/trace`, `cf/formatters`, `cf/i18n`, and `cf/flags`. The coupling hasn't broken plugins *yet* because the CLI team hasn't refactored these packages. Any future refactoring of `cf/terminal` or `cf/configuration` would break 8 plugins with no warning.

The one exception is `util/configv3` (mysql-cli-plugin only), which has 24 commits since 2020 including structural changes for Kubernetes support — this coupling has already diverged and would not compile against CLI HEAD.

The module path migration from `code.cloudfoundry.org/cli` to `code.cloudfoundry.org/cli/v8` is an additional breaking change for plugins still pinned to `v7.1.0+incompatible`.

**Why this matters for the transitional migration:** The V2Compat wrapper approach addresses the *intended* coupling (imports of `plugin/` and `plugin/models/`). But plugins with internal package imports have a second, harder coupling problem that the wrapper cannot solve — they must replace those imports with standalone alternatives.

#### Function-Level Complexity Assessment

A function-level audit of what each plugin actually calls reveals that **most coupling is shallow** — a handful of functions, not deep framework integrations. This makes replacement feasible for 7 of 9 packages with minimal effort.

**cf/terminal — two distinct usage patterns:**

| Pattern | Plugins | Functions Used | Replacement Effort |
|---|---|---|---|
| **Full UI framework** | multiapps-cli-plugin (16 files) | `terminal.UI` interface (`Say`, `Warn`, `Failed`, `Ok`, `Table`, `Ask`, `Confirm`), `NewUI`, `NewTeePrinter`, `EntityNameColor` (30+ calls), `CommandColor`, `FailureColor`, `InitColorSupport`, `UITable` | Medium-Large — but self-contained to one plugin |
| **UI bootstrap only** | ocf-scheduler, cf-java-plugin, Swisscom appcloud, html5-apps-repo, list-services | `NewUI`, `NewTeePrinter`, `UserAskedForColors`, `InitColorSupport` | Medium — replace with `text/tabwriter` |

**cf/trace — two functions total:**

| Function | Plugins | Call Sites | Purpose |
|---|---|---|---|
| `trace.NewLogger(writer, verbose, boolsOrPaths...)` | app-autoscaler, cf-app-autoscaler | HTTP transport wrappers | Actual tracing |
| `trace.NewWriterPrinter(writer, writesToConsole)` | multiapps, ocf-scheduler | Passed to `terminal.NewUI()` as required logger arg | Transitive dependency of `cf/terminal` — removing terminal removes this |

**cf/configuration/confighelpers — two functions total:**

| Function | Plugins | Purpose |
|---|---|---|
| `confighelpers.DefaultFilePath()` | cf-targets, app-autoscaler, cf-app-autoscaler, mysql-cli, cfc-cf-targets | Returns `~/.cf/config.json` path (respects `$CF_HOME`) |
| `confighelpers.PluginRepoDir()` | mysql-cli-plugin only | Returns plugin repo directory |

Replacement: ~5 lines of stdlib code (`$CF_HOME` fallback to `$HOME/.cf`).

**cf/i18n — one variable assignment, both plugins identical:**

```go
i18n.T = func(translationID string, args ...interface{}) string { return translationID }
```

Both multiapps and ocf-scheduler set `i18n.T` to a **no-op passthrough**. Neither plugin actually translates strings — they satisfy a dependency of `terminal.NewUI()`. Removing `cf/terminal` eliminates this import entirely.

**Remaining packages — single function each:**

| Package | Plugin | What's Used | Replacement |
|---|---|---|---|
| `cf/formatters` | multiapps | `ByteSize()` — 1 call site | Inline or `dustin/go-humanize` |
| `cf/flags` | Swisscom appcloud | `FlagContext` type — 1 struct field | stdlib `flag` or `pflag` |
| `util/ui` | mysql-cli-plugin | `NewUI()` → `DisplayBoolPrompt()`, `DisplayText()` — 1 confirmation prompt | `fmt.Print` + `bufio.Scanner` (~10 lines) |
| `util/configv3` | mysql-cli-plugin | `&configv3.Config{}` — empty struct passed to `ui.NewUI()` | Eliminated when `util/ui` is replaced |

**cf/configuration + coreconfig — the hard case (cf-targets-plugin only):**

`configuration.NewDiskPersistor()`, `coreconfig.NewData()`, `.JSONMarshalV3()` — reads and writes `~/.cf/config.json` directly to implement target switching. The config file format is undocumented and the CLI team is not bound to maintain compatibility. The plugin team has four options, each with trade-offs:

1. **Copy the code** into the plugin (or into `cf-plugin-helpers`). Eliminates the import dependency but the plugin now owns a copy of code that parses an undocumented format — same breakage risk if the CLI team changes the format, just without the compile-time signal.
2. **Keep the existing import**. Same tight coupling as today. Works as long as the `cf/configuration` packages remain frozen — which they have been since 2020, but is not guaranteed.
3. **Request CLI team provide a supported integration point** (documented format, plugin RPC method, or supported package). The only path to a stable contract, but depends on CLI team willingness.
4. **Incorporate the targets functionality into the CF CLI itself.** Target switching is a general-purpose workflow that benefits all CLI users, not just plugin consumers. If adopted as a native CLI feature, the plugin becomes unnecessary and the coupling problem is eliminated entirely.

The choice is a risk tolerance decision for the plugin team. The scanner SHOULD detect and report this coupling regardless of which option is chosen.

**cf/terminal Pattern A — the messy case (multiapps-cli-plugin only):**

multiapps uses the full `cf/terminal` UI framework across 16 production files: `terminal.UI` interface (`Say`, `Warn`, `Failed`, `Ok`, `Table`, `Ask`, `Confirm`), `EntityNameColor` (30+ calls), `CommandColor`, `FailureColor`, `TeePrinter`, `UITable`, and color initialization. The first three options from `cf/configuration` apply here as well, but copying is messier:

1. **Copy the code** into the plugin. Unlike `cf/configuration` (which is self-contained), `cf/terminal` has transitive dependencies on `cf/trace` (for the `Printer` interface required by `NewUI`), `cf/i18n` (for translated strings), and internal types — extracting it requires pulling in multiple packages. The `cfui` package in `cf-plugin-helpers` covers Pattern B basics (`Say`, `Warn`, `Failed`, `Ok`, `Table`, color functions), but multiapps' full usage exceeds what a generic helper provides.
2. **Keep the existing import**. Works while `cf/terminal` stays frozen — which it has since 2020. Same risk profile as cf-targets.
3. **Request CLI team extract `cf/terminal` into a standalone module**. Would benefit all 6 plugins that import it, not just multiapps. But depends on CLI team willingness, and the CLI team is not bound to maintain compatibility.

#### Replacement Complexity Summary

| Complexity | Packages | Approach |
|---|---|---|
| **Trivial** (1-5 lines stdlib) | `confighelpers`, `cf/formatters`, `cf/flags` | Direct replacement with stdlib or inline code |
| **Transitive** (eliminated automatically) | `cf/i18n`, half of `cf/trace` | Only imported because `terminal.NewUI()` requires them — removing terminal removes these |
| **Small** (10-30 lines) | `util/ui`, `cf/trace` (actual tracing) | `bufio.Scanner` for prompts; `log` or custom `Printer` interface for tracing |
| **Medium** (tabwriter + color) | `cf/terminal` Pattern B (5 plugins) | `text/tabwriter` for tables, optional `fatih/color` for colored output |
| **Medium-Large** (one plugin) | `cf/terminal` Pattern A (multiapps only) | 16 files, full UI framework — significant but scoped to one plugin team |
| **Hard** (design-level) | `cf/configuration` + `coreconfig`, `util/configv3` | Config file I/O with undocumented format — no clean drop-in replacement |

#### Addressing the Internal Import Coupling

For the 7 packages rated trivial through medium, standalone replacement packages with matching function signatures can eliminate the coupling entirely — plugins change import paths only, no code changes required. The `cf-plugin-helpers` module design and migration instructions are in the [Proposal: Decoupling Internal Imports via `cf-plugin-helpers`](#decoupling-internal-imports-via-cf-plugin-helpers) section. The 2 hard cases (`cf/configuration` + `coreconfig`, `cf/terminal` Pattern A) require case-by-case risk decisions described in the analysis above.

### Who Should Migrate

- Plugin developers whose plugins call V2 domain methods (`GetApp`, `GetApps`, `GetService`, `GetServices`, `GetOrg`, `GetOrgs`, `GetSpaces`, `GetOrgUsers`, `GetSpaceUsers`)
- Plugin developers who use `CliCommand`/`CliCommandWithoutTerminalOutput` for `cf curl` against CAPI endpoints
- Plugin developers who want to access CAPI V3 directly but currently bootstrap go-cfclient or custom HTTP clients manually

## Proposal

### Architecture

The transitional approach introduces a thin wrapper on the guest side:

```
┌─────────────────────────────────────────────────────┐
│                  Host (CF CLI)                       │
│                                                     │
│  Existing plugin.CliConnection via gob/net-rpc       │
│  (no changes required)                              │
└────────────────────────┬────────────────────────────┘
                         │
                         │  Existing gob/net-rpc protocol (Go's built-in binary RPC)
                         │
┌────────────────────────▼────────────────────────────┐
│                  Guest (Plugin)                      │
│                                                     │
│  ┌───────────────────────────────────────────────┐  │
│  │  V2Compat Wrapper (generated)                 │  │
│  │                                               │  │
│  │  ┌─────────────────┐  ┌───────────────────┐  │  │
│  │  │ Pass-through     │  │ go-cfclient V3    │  │  │
│  │  │ (context methods │  │ (constructed from │  │  │
│  │  │  delegate to     │  │  AccessToken +    │  │  │
│  │  │  original conn)  │  │  ApiEndpoint)     │  │  │
│  │  └─────────────────┘  └───────────────────┘  │  │
│  └───────────────────────────────────────────────┘  │
│                                                     │
│  Plugin domain logic uses go-cfclient V3 directly   │
└─────────────────────────────────────────────────────┘
```

The wrapper:
- **Embeds** `plugin.CliConnection` — all 16 context methods pass through unchanged
- **Constructs** a go-cfclient V3 `*client.Client` from `AccessToken()`, `ApiEndpoint()`, and `IsSSLDisabled()`
- **Reimplements** only the V2 domain methods the plugin actually uses, backed by the minimum V3 API calls required
- **Satisfies** `plugin.CliConnection` — existing code that accepts the connection interface works without changes

**What changes vs. what doesn't:**

| Component | Changes? | Details |
|---|---|---|
| `plugin.Start()` entry point | No | Standard host registration, unchanged |
| Context methods (`AccessToken`, `GetCurrentOrg`, etc.) | No | Pass through to host via RPC |
| V2 domain methods (`GetApp`, `GetApps`, etc.) | **Yes** | Reimplemented via V3 on guest side |
| `CliCommand` / `CliCommandWithoutTerminalOutput` | **Flagged** | Should migrate to go-cfclient; workflow commands can stay |
| Host CLI codebase | No | No host changes required |

### Host-Guest Separation Enables CLI Simplification

A key benefit of this migration is that it **decouples the guest's data needs from the host's implementation**. Today, when a plugin calls `conn.GetApp("myapp")`, the host must:

1. Receive the RPC call
2. Query CAPI (currently V2) on behalf of the plugin
3. Populate a `plugin_models.GetAppModel` struct
4. Serialize and return it via gob/net-rpc

After migration, the guest calls CAPI V3 directly. The host's role shrinks to providing authentication context (`AccessToken`, `ApiEndpoint`, `IsSSLDisabled`) and session state (`GetCurrentOrg`, `GetCurrentSpace`). These are simple, stable methods unlikely to change.

This means the CLI team can:
- Remove the V2 domain method RPC handlers from `plugin/rpc/cli_rpc_server.go`
- Remove the `plugin_models.*` types that mirror V2 response structures
- Simplify the plugin launch code in `plugin/rpc/run_plugin.go`
- Reduce the surface area for RPC-related bugs

The migration happens **without coordination** — each plugin developer migrates independently, and the CLI team removes host-side code when usage drops below their threshold.

#### Host-Side Alternative: Rewriting cli_rpc_server.go Against V3

An alternative approach rewrites the host's RPC handlers to call CAPI V3 internally while preserving the existing `plugin_models.*` wire format. This means plugins require zero changes — the host transparently switches from V2 to V3 backends. A proof-of-concept of this approach exists as [cloudfoundry/cli#3741](https://github.com/cloudfoundry/cli/pull/3741) (explicitly marked "DO NOT CONSIDER THIS PR FOR MERGING" — it is a discussion piece for [cli#3621](https://github.com/cloudfoundry/cli/issues/3621)).

**What the POC does:**

The POC rewrites 9 of 10 domain methods in `plugin/rpc/cli_rpc_server.go` to use `v7action.Actor` and `ccv3.Client` instead of the legacy `commandregistry` + V2 command runner pattern. It also rewrites `AccessToken` to use `v7action.RefreshAccessToken()` instead of the legacy `authenticationRepository`. `GetApp` is left on the legacy path because it requires coordinating 6+ V3 API calls to populate the full `GetAppModel`.

The implementation introduces:

- A **~120-line `configAdapter`** struct that bridges `coreconfig.Repository` (the legacy config interface) to the `configv3`-shaped interfaces required by `v7action.Actor`, `ccv3.Client`, `uaa.Client`, and `router.Client`. This adapter implements ~20 methods including `SkipSSLValidation`, `SetTargetInformation`, `CurrentUser`, and timeout accessors.
- A **~60-line `simpleRequestLogger`** implementing the `RequestLoggerOutput` interface for CF_TRACE support.
- A **~120-line `getClientsForActor`** function that constructs the CC v3, UAA, and routing clients with proper authentication wrappers.
- **Model mapping functions** that convert `resources.*` and `v7action.*` types to `plugin_models.*` structs.

**Comparison:**

| Aspect | Guest-side (this RFC) | Host-side (PR #3741 POC) |
|---|---|---|
| Who changes code | Plugin developers | CLI team |
| Plugin code changes | 1 line (`NewV2Compat(conn)`) | Zero |
| CLI code changes | Zero | ~800 lines in `cli_rpc_server.go` |
| New dependencies added | go-cfclient in each plugin | None (ccv3 already in CLI) |
| API call efficiency | Optimized per-plugin (only declared fields) | Fixed (all fields populated) |
| Field completeness | Per-plugin: only used fields populated | Incomplete: empty GUIDs, missing quotas, hardcoded `IsAdmin: false` |
| `GetApp` coverage | Full (generated per field group) | Skipped (left on legacy V2 path) |
| CF_TRACE visibility | `cftrace.NewTracingTransport` for guest calls | Supported via `simpleRequestLogger` |
| Legacy code removal | Enables removal of `cli_rpc_server.go` domain methods | Replaces V2 internals but preserves the RPC handler structure |
| Wire format coupling | Temporary (until plugin SDK) | Permanent (`plugin_models.*` remains the contract) |
| Test coverage | Golden file tests for generated code | POC removes 9 existing test cases without replacements |

**The approaches are not mutually exclusive.** The host-side rewrite prevents plugins from breaking when V2 is removed — buying time. The guest-side migration eliminates the coupling long-term — enabling the CLI team to eventually remove the domain method handlers entirely. A sequenced approach could use the host-side fix as a safety net while plugin teams migrate at their own pace:

1. CLI team lands host-side V3 rewrite (plugins don't break)
2. Plugin teams adopt guest-side migration (plugins decouple)
3. CLI team removes host-side domain handlers (maintenance burden eliminated)

**Why this RFC focuses on guest-side:** The host-side approach fixes the immediate V2 breakage but perpetuates the structural problem — the CLI remains responsible for populating V2-shaped domain models from V3 data, the `plugin_models.*` types remain part of the wire contract, and every new V3 concept (metadata labels, sidecars, rolling deployments) requires CLI team work to surface through the plugin interface. The guest-side approach eliminates the coupling entirely: each plugin team owns its data access, the CLI provides only stable context, and the CLI team's plugin maintenance burden drops to near zero.

The `configAdapter` in the POC illustrates this cost concretely: 120 lines of glue bridging two config systems (`coreconfig.Repository` → `configv3` interfaces) that exist only because the RPC server lives in the legacy `cf/` package tree while v7action lives in the modern `actor/` tree. This adapter would need to track changes in both config systems indefinitely. The guest-side approach avoids this entirely — go-cfclient constructs its own client from three primitives (`AccessToken`, `ApiEndpoint`, `IsSSLDisabled`).

**Build-time dependency remains.** The runtime decoupling described above does not eliminate the plugin's compile-time dependency on the CLI's Go packages. Plugins still import `code.cloudfoundry.org/cli/plugin` (for the `CliConnection` interface and `plugin.Start()`) and `code.cloudfoundry.org/cli/plugin/models` (for the V2 model types the generated wrappers return). These are the *intended* public plugin contract — not internal packages — but they still couple the guest's build to the host's repository.

#### Achieving Full Build-Time Separation: Plugin SDK

> **TODO:** The following analysis describes the planned approach for full build-time decoupling. This will be implemented before the RFC approval process.

The build-time coupling can be eliminated by publishing a **plugin SDK** — a standalone Go module owned by the plugin side — containing copies of the host's plugin interface types:

- `plugin.Plugin` interface, `plugin.CliConnection` interface
- `plugin.Start()` function (the plugin-side RPC client setup)
- `plugin.PluginMetadata`, `plugin.Command`, `plugin.Usage`, `plugin.VersionType` types
- `plugin/models/*` — all V2 model types (`GetAppModel`, `GetAppsModel`, etc.)

**Why copying works:** The plugin and CLI are separate processes communicating via gob/net-rpc over TCP. The contract between them is the **wire format** — gob encodes structs by field name and type, not by Go package path. As long as both sides define identical struct layouts, gob serialization works correctly across the process boundary regardless of which Go module the types live in.

**Ownership model:** Today the CLI owns the interface definition and plugins are consumers. The SDK flips this — plugins own the interface, and the CLI implements the wire protocol against it. This is appropriate because the plugin interface is a *contract boundary*, not an internal implementation detail.

**Migration sequence:**

| Step | Who | What Changes |
|---|---|---|
| 1. Publish plugin SDK | Plugin side | SDK published with copies of current `plugin/` and `plugin/models/` types |
| 2. Plugins update imports | Plugin developers | Import paths change from `cli/plugin` to SDK — no logic changes |
| 3. `cf-plugin-migrate` generates SDK imports | Tool | Generated wrappers import SDK types instead of CLI types |
| 4. CLI keeps its own copy | CLI team | Nothing changes — no coordination needed |
| 5. CLI removes its copy (when ready) | CLI team | CLI imports the plugin SDK for the types it still needs (RPC server, gob encoding) |
| 6. V3 interface ships | Both | New V3 contract replaces V2 types; V2 SDK becomes legacy |

Steps 1–3 require no CLI team involvement. Steps 4–5 happen on the CLI team's timeline. The plugin side is fully decoupled after step 2.

**Risk:** If either side changes a struct field without the other matching, the gob wire format breaks. This risk is low — these types have been frozen for years, and the explicit plan is to not change them until the V3 plugin interface is defined.

**What this buys:** After step 2, plugins no longer import the CLI repository at all. The `go.mod` dependency on `code.cloudfoundry.org/cli` disappears. Plugins build faster, have fewer transitive dependencies, and are immune to CLI module restructuring.

### The Scanner: Automated V2 Usage Audit

#### What It Does

`cf-plugin-migrate scan` is an AST-based audit tool that analyzes a plugin's Go source code and produces a complete inventory of V2 interface usage. It answers the question every plugin developer and migration planner needs answered: **what exactly does this plugin depend on from the V2 interface?**

The scanner detects four categories of usage:

1. **V2 domain method calls** — `GetApp`, `GetApps`, `GetService`, `GetServices`, `GetOrg`, `GetOrgs`, `GetSpace`, `GetSpaces`, `GetOrgUsers`, `GetSpaceUsers`. For each call, it traces which fields of the returned model are actually accessed (e.g., only `Guid` and `Name` out of 20+ available fields on `GetAppModel`).

2. **`CliCommand` / `CliCommandWithoutTerminalOutput` calls** — Every call is detected with its command name and arguments. For `cf curl` calls, the scanner performs deep analysis: endpoint URL extraction, `json.Unmarshal` tracing, target struct type detection, field access tracking, and V2→V3 endpoint mapping.

3. **Session/context method calls** — `AccessToken`, `ApiEndpoint`, `GetCurrentOrg`, `GetCurrentSpace`, etc. These pass through unchanged and require no migration.

4. **Internal CLI package imports** — imports of `code.cloudfoundry.org/cli/cf/*`, `util/*`, or other non-`plugin` packages. For each detected import, the scanner outputs the replacement `cf-plugin-helpers` package path when one exists (see [Decoupling Internal Imports via `cf-plugin-helpers`](#decoupling-internal-imports-via-cf-plugin-helpers)).

**Output:**
- **Human-readable summary** (stderr) — for quick review
- **YAML configuration** (stdout) — `cf-plugin-migrate.yml`, ready for the code generator

```bash
$ cf-plugin-migrate scan ./...
Found V2 domain method calls:

  commands/create-job.go:42  GetApp  → fields: Guid
  core/util.go:15            GetApps → fields: Guid, Name
    V3 API calls: Applications.Single, Applications.ListAll

Found CliCommand calls (legacy — not available in V3 plugin interface):

  client.go:92  CliCommandWithoutTerminalOutput("curl", "v2/apps")
    → V3 equivalent: /v3/apps (V2 entity/metadata envelope → V3 flat resources)
    → Unmarshalled into: apps (AppsModel)
    → Fields used: NextURL, Resources

Internal CLI package imports detected:

  code.cloudfoundry.org/cli/cf/trace
    → Replace with: trace "code.cloudfoundry.org/cf-plugin-helpers/cftrace"
    Note: import alias preserves existing trace.X references

  code.cloudfoundry.org/cli/cf/configuration/confighelpers
    → Replace with: confighelpers "code.cloudfoundry.org/cf-plugin-helpers/cfconfig"
    Note: import alias preserves existing confighelpers.X references
```

#### How the YAML Maps V2 Fields to V3 API Calls

The key insight is that most plugins use only a fraction of the fields available on V2 model types. The YAML captures exactly which fields are used, and the generator maps each field to the minimum V3 API call required:

```yaml
# cf-plugin-migrate.yml — generated by scan, consumed by generate
schema_version: 1
package: main
methods:
  GetApp:
    fields: [Guid, Name]        # ← only 2 of 20+ fields used
  GetApps:
    fields: [Guid, Name, State] # ← only 3 of 8 fields used
```

Each field belongs to a **dependency group** — a set of V3 API calls. The generator includes a group only when at least one field from that group is requested:

| Fields Requested | V3 API Call | Call Count |
|---|---|---|
| `Guid`, `Name`, `State`, `SpaceGuid` | `Applications.Single()` | 1 |
| + `Routes` | + `Routes.ListForApp(include=domain)` | 2 |
| + `Command`, `Memory`, `InstanceCount` | + `Processes.ListForApp()` | 3 |
| + `RunningInstances` | + `Processes.GetStats()` | 4 |
| + `BuildpackUrl` | + `Droplets.GetCurrentForApp()` | 5 |

If a plugin only uses `Guid` and `Name`, the generated code makes **1 API call**. Compare this to the Rabobank library which always makes 10+ calls to populate every field regardless of usage.

#### Scanner Limitations (and Why They Don't Matter)

The scanner uses static AST analysis without full type resolution. This means there are patterns it cannot trace:

| Pattern | Example | Scanner Handles? |
|---|---|---|
| Direct field access | `app.Guid`, `app.Routes[0].Host` | **Yes** |
| Range iteration | `for _, s := range services { s.Name }` | **Yes** |
| Indexed access | `app.Routes[i].Domain.Name` | **Yes** |
| Passed to helper function | `helper(app)` then `app.Guid` in helper | **Flagged** — cross-function |
| Stored in struct field | `ctx.App = app` then `ctx.App.Guid` | **Flagged** — alias tracking |
| Reflection / interface cast | `reflect.ValueOf(app).FieldByName(...)` | **No** |

**Why the limitations don't matter:**

1. **Conservative is correct.** The scanner flags ambiguous cases for manual review rather than silently omitting them. A developer reviewing the YAML can add any missed fields in seconds.

2. **The common case is simple.** Analysis of 18 actively maintained plugins shows that the vast majority of field access happens directly at or near the call site. The OCF Scheduler, cf-targets-plugin, and Rabobank consumers all follow this pattern.

3. **The YAML is editable.** The scan output is a starting point, not a final answer. Developers can add fields, remove false positives, or adjust sub-field specifications before generating code.

4. **Reflection-based access doesn't exist in practice.** No surveyed plugin uses reflection to access V2 model fields.

### Migration Guide

#### Path A: Generated V2Compat Wrapper (Recommended)

This is the recommended approach for most plugins. The generated wrapper satisfies the `plugin.CliConnection` interface, so existing plugin code works without modification.

**Step 1: Scan**

```bash
cd your-plugin/
go run cf-plugin-migrate scan ./...  > cf-plugin-migrate.yml
```

Review the YAML output. The scanner auto-detects V2 domain method calls and the specific fields your plugin accesses.

**Step 2: Generate**

```bash
go run cf-plugin-migrate generate
```

This reads `cf-plugin-migrate.yml` and produces `v2compat_generated.go` — a single file containing:
- A `V2Compat` struct that embeds `plugin.CliConnection`
- A `NewV2Compat(conn)` constructor that builds a go-cfclient V3 client from the connection's credentials
- Reimplementations of only the V2 domain methods your plugin uses, making only the V3 API calls required for the fields you access
- Pass-through implementations for all other methods

**Step 3: Add the dependency** (domain method plugins only)

```bash
go get github.com/cloudfoundry/go-cfclient/v3
```

Session-only plugins (those that use only context methods) need no new dependency — the generated file contains only pass-through methods.

**Step 4: Drop in and build**

For **session-only plugins** — you're done. The generated file compiles alongside your existing code with zero changes. Example: cf-targets-plugin required zero code changes.

For **domain method plugins** — you have two options for wiring the wrapper:

**Option A: Shadow the connection parameter (one line)**

```go
func (p *MyPlugin) Run(cliConnection plugin.CliConnection, args []string) {
    cliConnection, err := NewV2Compat(cliConnection) // shadow the parameter
    if err != nil {
        fmt.Println(err)
        return
    }
    // All existing code works unchanged — cliConnection.GetApp() now uses V3
    app, err := cliConnection.GetApp("myapp")
    // ...
}
```

**Option B: Explicit variable (more visible)**

```go
func (p *MyPlugin) Run(conn plugin.CliConnection, args []string) {
    compat, err := NewV2Compat(conn)
    if err != nil {
        fmt.Println(err)
        return
    }
    // Pass compat wherever the plugin expects plugin.CliConnection
    app, err := compat.GetApp("myapp")
    // ...
}
```

Both options work because `*V2Compat` satisfies `plugin.CliConnection`. The plugin developer chooses based on their preference for explicitness.

**What the generated code looks like:**

For a plugin that uses only `GetApp` with fields `[Guid, Name]`:

```go
// GENERATED by cf-plugin-migrate — do not edit
func (c *V2Compat) GetApp(name string) (plugin_models.GetAppModel, error) {
    var model plugin_models.GetAppModel
    space, err := c.GetCurrentSpace()
    if err != nil {
        return model, err
    }
    app, err := c.cfClient.Applications.Single(context.Background(),
        &client.AppListOptions{
            Names:      client.Filter{Values: []string{name}},
            SpaceGUIDs: client.Filter{Values: []string{space.Guid}},
        })
    if err != nil {
        return model, err
    }
    model.Guid = app.GUID
    model.Name = app.Name
    return model, nil
}
```

One V3 API call. Two fields populated. All other fields remain zero-valued. The generated comment documents exactly which fields are populated and how many API calls are made.

#### Path B: Direct V3 Access (For New Development)

For plugins starting fresh or developers who want to use V3-native types directly, bypass the V2 compatibility wrapper entirely:

```go
func (p *MyPlugin) Run(conn plugin.CliConnection, args []string) {
    // Construct a go-cfclient V3 client from the connection's credentials
    endpoint, _ := conn.ApiEndpoint()
    token, _ := conn.AccessToken()
    skipSSL, _ := conn.IsSSLDisabled()

    cfg, _ := config.New(endpoint,
        config.Token(token),
        config.SkipTLSValidation(skipSSL),
    )
    cfClient, _ := client.New(cfg)

    // Use V3-native types — full model with metadata, lifecycle, relationships
    app, _ := cfClient.Applications.Single(context.Background(),
        &client.AppListOptions{
            Names: client.Filter{Values: []string{"my-app"}},
        })
    fmt.Printf("App: %s (%s)\n", app.Name, app.GUID)

    // V3 gives access to processes, sidecars, metadata — not available via V2
    processes, _, _ := cfClient.Processes.ListForApp(context.Background(), app.GUID, nil)
    for _, proc := range processes {
        fmt.Printf("  Process: %s, Instances: %d\n", proc.Type, proc.Instances)
    }
}
```

**When to choose Path B:**
- New plugin development
- Plugins that need V3-only concepts (multiple process types, sidecars, metadata labels)
- Developers comfortable rewriting call sites

**V2 to V3 method mapping** (for manual migration):

| V2 Method | V3 Replacement (go-cfclient) |
|---|---|
| `conn.GetApp(name)` | `cfClient.Applications.Single(ctx, &client.AppListOptions{Names: ..., SpaceGUIDs: ...})` |
| `conn.GetApps()` | `cfClient.Applications.ListAll(ctx, &client.AppListOptions{SpaceGUIDs: ...})` |
| `conn.GetService(name)` | `cfClient.ServiceInstances.Single(ctx, &client.ServiceInstanceListOptions{Names: ..., SpaceGUIDs: ...})` |
| `conn.GetServices()` | `cfClient.ServiceInstances.ListAll(ctx, &client.ServiceInstanceListOptions{SpaceGUIDs: ...})` |
| `conn.GetOrg(name)` | `cfClient.Organizations.Single(ctx, &client.OrganizationListOptions{Names: ...})` |
| `conn.GetOrgs()` | `cfClient.Organizations.ListAll(ctx, nil)` |
| `conn.GetSpaces()` | `cfClient.Spaces.ListAll(ctx, &client.SpaceListOptions{OrganizationGUIDs: ...})` |

Context methods (`AccessToken`, `ApiEndpoint`, `GetCurrentOrg`, `GetCurrentSpace`, `Username`, `IsLoggedIn`, `HasOrganization`, `HasSpace`, `IsSSLDisabled`, `HasAPIEndpoint`, `ApiVersion`) continue to use the existing `plugin.CliConnection` — these are the core contract methods that any future plugin interface preserves.

#### CliCommand / cf curl Migration

The scanner detects all `CliCommand` and `CliCommandWithoutTerminalOutput` calls and categorizes them:

**`cf curl` calls** — SHOULD migrate to go-cfclient or direct HTTP. The scanner provides V2→V3 endpoint mapping for 20 known V2 API paths:

```go
// Before:
output, _ := conn.CliCommandWithoutTerminalOutput("curl", "/v2/apps?q=name:myapp")
// Parse JSON from output[0]

// After:
apps, _ := cfClient.Applications.ListAll(ctx,
    &client.AppListOptions{Names: client.Filter{Values: []string{"myapp"}}})
```

**Workflow commands** (`push`, `bind-service`, `restage`, `delete`) — MAY continue to use `CliCommand` during the transition. These are multi-step workflow operations managed by the CLI. They can be replaced with go-cfclient calls as a future optimization, but they work correctly as-is and do not depend on V2 endpoints.

| CLI Command | Can migrate to go-cfclient? | Priority |
|---|---|---|
| `create-user-provided-service` | Yes: `cfClient.ServiceInstances.CreateUserProvided()` | Low — CLI handles it fine |
| `bind-service` | Yes: `cfClient.ServiceCredentialBindings.Create()` | Low |
| `unbind-service` | Yes: `cfClient.ServiceCredentialBindings.Delete()` | Low |
| `delete-service` | Yes: `cfClient.ServiceInstances.Delete()` | Low |

#### Worked Examples

##### cf-targets-plugin (Session-Only: Zero Code Changes)

The cf-targets-plugin uses only context methods (`GetCurrentOrg`, `GetCurrentSpace`, `HasOrganization`, `HasSpace`). No V2 domain methods, no `CliCommand` calls.

**Migration:** Run `cf-plugin-migrate scan` → generates a session-only V2Compat with pure pass-through methods. Drop in the file, build, install. **Zero lines of plugin code changed.**

##### list-services (Tier 1: Simplest Domain Method Migration)

The [list-services plugin](https://github.com/pavellom/list-services-plugin) lists services bound to a specific app. It is **already 90% V3** — it uses `GetApp(name)` solely to resolve an app name to a GUID, then immediately calls CAPI V3 via `cf curl /v3/service_bindings?app_guids={guid}` to fetch the actual data.

**Current usage:**

| Interface | Call | Fields/Details |
|---|---|---|
| V2 domain method | `GetApp(appName)` | `.Guid` only — name-to-GUID resolution |
| `CliCommandWithoutTerminalOutput` | `curl /v3/service_bindings?app_guids={guid}` | V3 endpoint — paginated, JSON parsed manually |
| `CliCommand` | `help list-services` | Help text display (plugin's own command) |
| Context methods | `IsLoggedIn()`, `HasOrganization()`, `HasSpace()` | Login/target guards |
| CLI internal imports | `cf/terminal`, `cf/trace` | Table formatting and tracing |

**Scan output:**

```yaml
schema_version: 1
package: main
methods:
  GetApp:
    fields: [Guid]
cli_commands:
  - file: listservices.go
    method: CliCommandWithoutTerminalOutput
    command: curl
    endpoint: /v3/service_bindings
    v3_endpoint: /v3/service_credential_bindings  # renamed in newer CAPI
```

**Migration — three independent improvements:**

**1. Replace `GetApp` (V2 domain method → V3):**

```go
// Before:
app, err := cliConnection.GetApp(appName)
// uses app.Guid

// After (generated wrapper):
cliConnection, err := NewV2Compat(cliConnection)
// app.Guid now resolved via V3 Applications.Single()

// After (direct V3 — recommended for this plugin):
apps, err := cfClient.Applications.ListAll(ctx,
    &client.AppListOptions{
        Names:      client.Filter{Values: []string{appName}},
        SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
    })
appGUID := apps[0].GUID
```

Since the plugin uses only `.Guid`, either approach works. The direct V3 call is simpler here because the plugin already does its own HTTP calls for everything else.

**2. Replace `cf curl` with go-cfclient (optional but recommended):**

```go
// Before:
output, _ := cliConnection.CliCommandWithoutTerminalOutput("curl",
    fmt.Sprintf("/v3/service_bindings?app_guids=%s", app.Guid))
// Manual JSON parsing, manual pagination loop

// After:
bindings, err := cfClient.ServiceCredentialBindings.ListAll(ctx,
    &client.ServiceCredentialBindingListOptions{
        AppGUIDs: client.Filter{Values: []string{appGUID}},
        Include:  client.ServiceCredentialBindingIncludeServiceInstance,
    })
for _, b := range bindings {
    // b.Relationships.ServiceInstance gives the instance GUID
    // Included service instance gives the name
}
```

This eliminates the manual `Response`/`Pagination`/`Resource` structs, the pagination loop, and the JSON parsing. go-cfclient handles all of it.

**3. Replace `cf/terminal` import (host-code coupling):**

```go
// Before (terminal/terminal.go):
import "code.cloudfoundry.org/cli/cf/terminal"
ui := terminal.NewUI(os.Stdin, os.Stdout, terminal.NewTeePrinter(os.Stdout), trace.NewWriterPrinter(os.Stdout, false))
table := ui.Table([]string{"Service Name", "Service URL"})
table.Add(name, url)
table.Print()

// After (standard library):
w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
fmt.Fprintln(w, "Service Name\tService URL")
for _, b := range bindings {
    fmt.Fprintf(w, "%s\t%s\n", b.Name, b.URL)
}
w.Flush()
```

This removes the `cf/terminal` and `cf/trace` coupling entirely, replacing it with Go's standard library `text/tabwriter`.

**Result:**

| Metric | Before | After |
|---|---|---|
| V2 domain methods | 1 (`GetApp`) | **0** |
| `cf curl` calls | 2 (V3 endpoint + pagination) | **0** (go-cfclient handles pagination) |
| CLI internal imports | 2 (`cf/terminal`, `cf/trace`) | **0** (standard library) |
| Manual JSON structs | 5 (`Response`, `Pagination`, `Resource`, `Data`, `Links`) | **0** |
| New dependencies | — | go-cfclient/v3 |

**Why this is the ideal Tier 1 example:** The plugin demonstrates all three coupling patterns found across the plugin ecosystem — V2 domain methods, `cf curl` calls, and CLI internal package imports — but each in its simplest form. The migration is straightforward because the plugin already uses V3 for its core data access; it just needs the last V2 dependency (`GetApp` for GUID resolution) replaced and the CLI internal imports removed.

##### OCF Scheduler (Domain Methods: One Dependency + One Line)

The [OCF Scheduler plugin](https://github.com/cloudfoundry-community/ocf-scheduler-cf-plugin) uses `GetApp` (for `.Guid` only) and `GetApps` (for `.Guid`, `.Name`). No other fields are accessed — not `State`, `Routes`, `Memory`, `Instances`, or any other model attribute. The plugin uses V2 model methods purely as a name/GUID mapping layer.

**Scan output:**

```yaml
schema_version: 1
package: main
methods:
  GetApp:
    fields: [Guid]
  GetApps:
    fields: [Guid, Name]
```

**Generated code — `v2compat_generated.go`:**

```go
// GENERATED by cf-plugin-migrate — do not edit
//
// Source: cf-plugin-migrate.yml
// V3 API calls: Applications.Single (1 call per GetApp),
//               Applications.ListAll (1 call per GetApps)

package main

import (
	"context"

	"code.cloudfoundry.org/cli/plugin/models"
	"github.com/cloudfoundry/go-cfclient/v3/client"
)

// getApp resolves an app name to a GetAppModel.
// Populates: Name, Guid (1 V3 API call).
func getApp(cfClient *client.Client, spaceGUID string, name string) (plugin_models.GetAppModel, error) {
	var model plugin_models.GetAppModel

	app, err := cfClient.Applications.Single(context.Background(),
		&client.AppListOptions{
			Names:      client.Filter{Values: []string{name}},
			SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
		})
	if err != nil {
		return model, err
	}

	model.Name = app.Name
	model.Guid = app.GUID
	return model, nil
}

// getApps lists all apps in a space.
// Populates: Name, Guid (1 V3 API call).
func getApps(cfClient *client.Client, spaceGUID string) ([]plugin_models.GetAppsModel, error) {
	apps, err := cfClient.Applications.ListAll(context.Background(),
		&client.AppListOptions{
			SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
		})
	if err != nil {
		return nil, err
	}

	result := make([]plugin_models.GetAppsModel, len(apps))
	for i, app := range apps {
		result[i].Name = app.Name
		result[i].Guid = app.GUID
	}
	return result, nil
}
```

**Call site changes — 3 lines per site:**

`commands/create-job.go` (and identically `create-call.go`):
```go
// Before:
app, err := services.CLI.GetApp(appName)

// After:
app, err := getApp(services.CfClient, services.SpaceGUID, appName)
```

`core/util.go`:
```go
// Before:
func MyApps(services *Services) ([]models.GetAppsModel, error) {
    return services.CLI.GetApps()
}

// After:
func MyApps(services *Services) ([]models.GetAppsModel, error) {
    return getApps(services.CfClient, services.SpaceGUID)
}
```

The `Services` struct gains a `CfClient` and `SpaceGUID`, initialized once at startup:

```go
// Before:
type Services struct {
    CLI    plugin.CliConnection
    Client core.Driver
    UI     *ui.UI
}

// After:
type Services struct {
    CLI       plugin.CliConnection
    Client    core.Driver
    UI        *ui.UI
    CfClient  *client.Client   // go-cfclient V3
    SpaceGUID string           // cached from GetCurrentSpace()
}
```

**Migration:**
1. `go get github.com/cloudfoundry/go-cfclient/v3`
2. Drop in generated `v2compat_generated.go`
3. Add one line: `cliConnection, err := NewV2Compat(cliConnection)`

**Result:**

| Metric | Before (V2) | After (Generated) | Rabobank equivalent |
|---|---|---|---|
| V3 API calls for `GetApp()` | 0 (host handles it) | **1** | 11 |
| V3 API calls for `GetApps()` | 0 (host handles it) | **1** | 11 × N |
| Fields populated | All (unused) | **2** (Name, Guid) | All (unused) |
| New dependencies | None | go-cfclient/v3 | go-cfclient/v3 |
| Lines of generated code | N/A | **~40** | ~500 (full reimplementation) |
| Plugin code changes | N/A | **~10 lines** | ~10 lines |

The `AppByGUID` helper in `core/util.go` requires no changes — it already operates on `models.GetAppsModel` which the generated `getApps()` returns. All 6 command files that call `MyApps()` → `AppByGUID()` continue to work unchanged.

**Validated:** Built, installed via `cf install-plugin`, ran `cf create-job` against live CAPI V3 v3.180.0 — `GetApp` correctly resolved app name to GUID via V3.

##### metric-registrar (Complex: Domain Methods + Curl Calls)

The [metric-registrar plugin](https://github.com/pivotal-cf/metric-registrar-cli) is the most V2-coupled active plugin in the survey. It uses three V2 domain methods **and** four V2 CAPI curl calls. This analysis covers the migration in two phases: domain methods first, then curl calls.

**Phase 1: Domain Methods**

| V2 Method | Call Sites | Fields Accessed | V3 API Calls |
|---|---|---|---|
| `GetApp(name)` | `register.go` (1), `unregister.go` (2) | `Guid`, `Name`, `Routes[].Host`, `Routes[].Domain.Name`, `Routes[].Port`, `Routes[].Path` | 2 (app + routes) |
| `GetApps()` | `list.go` (2 — log formats + metrics endpoints) | `Guid`, `Name` | 1 |
| `GetServices()` | `register.go` (1) | `Name` only | 1 (server-side filtered) |

**Complexity difference from OCF Scheduler:** `GetApp()` here accesses **Routes with sub-fields** — this is the first case that needs a second V3 API call beyond `Applications.Single()`.

**Configuration — `cf-plugin-migrate.yml`:**

```yaml
# metric-registrar-cli/cf-plugin-migrate.yml
package: command
methods:
  GetApp:
    fields: [Name, Guid, Routes]
    route_fields: [Host, Domain.Name, Port, Path]
  GetApps:
    fields: [Name, Guid]
```

Note: `GetServices()` uses only `.Name` — the generator could produce a wrapper, but this is simple enough to replace directly with `cfClient.ServiceInstances.ListAll()`.

**Generated code — `v2compat_generated.go`:**

```go
// GENERATED by cf-plugin-migrate — do not edit
//
// Source: cf-plugin-migrate.yml
// V3 API calls: Applications.Single + Routes.ListForApp (2 calls per GetApp),
//               Applications.ListAll (1 call per GetApps)

package command

import (
	"context"

	"code.cloudfoundry.org/cli/plugin/models"
	"github.com/cloudfoundry/go-cfclient/v3/client"
)

// getApp resolves an app name to a GetAppModel.
// Populates: Name, Guid, Routes (2 V3 API calls).
func getApp(cfClient *client.Client, spaceGUID string, name string) (plugin_models.GetAppModel, error) {
	var model plugin_models.GetAppModel

	app, err := cfClient.Applications.Single(context.Background(),
		&client.AppListOptions{
			Names:      client.Filter{Values: []string{name}},
			SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
		})
	if err != nil {
		return model, err
	}

	model.Name = app.Name
	model.Guid = app.GUID

	// Routes — requested by developer (adds 1 API call)
	routes, _, err := cfClient.Routes.ListForApp(context.Background(), app.GUID, nil)
	if err != nil {
		return model, err
	}
	for _, r := range routes {
		route := plugin_models.GetApp_RouteSummary{
			Host: r.Host,
			Path: r.Path,
			Port: r.Port,
			Domain: plugin_models.GetApp_DomainFields{
				Name: r.DomainName(),
			},
		}
		model.Routes = append(model.Routes, route)
	}

	return model, nil
}

// getApps lists all apps in a space.
// Populates: Name, Guid (1 V3 API call).
func getApps(cfClient *client.Client, spaceGUID string) ([]plugin_models.GetAppsModel, error) {
	apps, err := cfClient.Applications.ListAll(context.Background(),
		&client.AppListOptions{
			SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
		})
	if err != nil {
		return nil, err
	}

	result := make([]plugin_models.GetAppsModel, len(apps))
	for i, app := range apps {
		result[i].Name = app.Name
		result[i].Guid = app.GUID
	}
	return result, nil
}
```

**`GetServices()` replacement — direct, no generator needed:**

```go
// Before (register.go):
existingServices, err := cliConn.GetServices()
for _, s := range existingServices {
    if s.Name == serviceName { ... }
}

// After:
instances, err := cfClient.ServiceInstances.ListAll(ctx,
    &client.ServiceInstanceListOptions{
        Names:      client.Filter{Values: []string{serviceName}},
        SpaceGUIDs: client.Filter{Values: []string{spaceGUID}},
        Type:       "user-provided",
    })
exists := len(instances) > 0
```

The V3 replacement is actually *better* — it filters server-side by name instead of fetching all services and scanning client-side.

**Phase 1 result:**

| Metric | Before (V2) | After (Generated) |
|---|---|---|
| V3 API calls for `GetApp()` | 0 (host handles it) | **2** (app + routes) |
| V3 API calls for `GetApps()` | 0 (host handles it) | **1** |
| V3 API calls for `GetServices()` | 0 (host handles it) | **1** (server-side filtered) |
| Fields populated in GetAppModel | All | **6** (Name, Guid, Routes.Host/Domain.Name/Port/Path) |
| New dependencies | None | go-cfclient/v3 |

Phase 1 can be completed independently. The plugin continues to use `CliCommandWithoutTerminalOutput("curl", ...)` for the V2 endpoints until Phase 2.

**Phase 2: Replacing V2 `cf curl` Calls**

The plugin makes four V2 curl calls. Two migrate cleanly; two require a structural redesign.

**V2→V3 endpoint mapping:**

| V2 Endpoint | V3 Equivalent | Migration Difficulty |
|---|---|---|
| GET `/v2/user_provided_service_instances?q=space_guid:{guid}` | GET `/v3/service_instances?type=user-provided&space_guids={guid}` | **Clean.** `syslog_drain_url` is a top-level field in V3. |
| GET `/v2/.../service_bindings` | GET `/v3/service_credential_bindings?service_instance_guids={guid}&type=app` | **Clean.** App GUID at `relationships.app.data.guid` instead of `entity.app_guid`. |
| GET `/v2/apps/{guid}` (read `.entity.ports`) | GET `/v3/apps/{guid}/routes` + GET `/v3/routes/{route-guid}/destinations` | **Structural change.** Ports moved from app entity to route destinations. |
| PUT `/v2/apps/{guid}` `{"ports":[...]}` | POST/PATCH `/v3/routes/{route-guid}/destinations` | **Structural change.** Must know route GUID, construct destination objects per route. |

**Clean migrations (registrations/fetcher.go):**

The fetcher currently pages through V2 UPS instances and their bindings:

```go
// Before — V2 curl:
path := fmt.Sprintf("/v2/user_provided_service_instances?q=space_guid:%s", space.Guid)
// Parses: resources[].entity.{name, syslog_drain_url, service_bindings_url}
// Then follows service_bindings_url to get: resources[].entity.app_guid

// After — go-cfclient V3:
instances, err := cfClient.ServiceInstances.ListAll(ctx,
    &client.ServiceInstanceListOptions{
        SpaceGUIDs: client.Filter{Values: []string{space.Guid}},
        Type:       "user-provided",
    })
// syslog_drain_url is instance.SyslogDrainURL (top-level field)

// For each instance, get bindings:
bindings, err := cfClient.ServiceCredentialBindings.ListAll(ctx,
    &client.ServiceCredentialBindingListOptions{
        ServiceInstanceGUIDs: client.Filter{Values: []string{instance.GUID}},
        Type:                 "app",
    })
// app GUID is binding.Relationships.App.Data.GUID
```

This replaces the manual `getPagedResource()` pagination, JSON parsing, and URL-following with typed SDK calls. The `paginatedResp`, `servicesResponse`, `serviceEntity`, `bindingsResponse`, and `bindingEntity` types can all be deleted.

**Structural change (ports/ports.go):**

The V2 app entity had a flat `ports` array. In V3, ports are a property of **route destinations** — the mapping between a route and an app process.

```go
// Before — V2: read ports from app entity
// GET /v2/apps/{guid} → .entity.ports → [8080, 9090]

// After — V3: read ports from route destinations
routes, _, err := cfClient.Routes.ListForApp(ctx, appGUID, nil)
var ports []int
for _, route := range routes {
    dests, _, err := cfClient.RouteDestinations.ListForRoute(ctx, route.GUID, nil)
    for _, dest := range dests {
        if dest.App.GUID == appGUID {
            ports = append(ports, dest.Port)
        }
    }
}
```

```go
// Before — V2: set ports on app entity
// PUT /v2/apps/{guid} {"ports": [8080, 9090]}

// After — V3: add destination to route
// Must first identify the correct route, then add a destination with the desired port
_, err := cfClient.RouteDestinations.Add(ctx, routeGUID, client.RouteDestinationAddOptions{
    Destinations: []client.RouteDestination{
        {
            App:      client.RouteDestinationApp{GUID: appGUID, Process: &client.Process{Type: "web"}},
            Port:     9090,
            Protocol: "http1",
        },
    },
})
```

**The port migration is a redesign, not a substitution.** The plugin currently thinks of ports as a property of the app. In V3, ports are a property of the route-to-app mapping. The plugin's `ports/ports.go` module needs:

1. A way to discover the app's routes (which it already has from `GetApp().Routes` after Phase 1)
2. Logic to map between the V2 flat-ports model and V3 per-route destinations
3. Handling for apps with multiple routes (each route has independent destinations)

This is the kind of migration that a generated wrapper **cannot solve** — it requires understanding the V3 domain model. The recommended approach is to rewrite `ports/ports.go` using go-cfclient's `Routes` and `RouteDestinations` resources directly.

#### Deep Analysis: V2 Ports → V3 Route Destinations

The V2 `ports` array on the app entity (`PUT /v2/apps/{guid} {"ports": [8080, 9090]}`) has **no direct equivalent in V3**. This is a fundamental architectural change, not a field rename:

| Concept | V2 | V3 |
|---|---|---|
| Declare app can serve on a port | `PUT /v2/apps/{guid}` with `ports` array | No equivalent — not needed |
| Route traffic to a specific port | Route mapping with `app_port` | Route destination with `port` field |
| Default port | 8080 (buildpack) or Docker EXPOSE | Same defaults, but declared per-destination |
| Read which ports are mapped | `GET /v2/route_mappings` | `GET /v3/routes/{route_guid}/destinations` |
| Atomic read-modify-write | Single PUT on app entity | Per-route: discover routes → manage destinations per route |

**What metric-registrar actually does with ports:**

The `--internal-port` flag does **not** create CF internal routes (`apps.internal` domain). "Internal" here means "a port internal to the container" — the platform-side metrics scraper reaches the app on that port directly via the container overlay network IP, bypassing the gorouter entirely.

The port lifecycle:
1. **Register:** Read current `entity.ports` (e.g., `[8080]`), append the metrics port (e.g., `[8080, 2112]`), write back via `PUT /v2/apps/{guid}`
2. **Unregister:** Read current ports, compute which ports are still needed by remaining registrations, write reduced array

The V2 `ports` array served a dual purpose: it told Diego which ports to expose on the container *and* which ports could be targeted by route mappings. In V3, these concerns are split:

- **Routable ports** are managed via route destinations (`POST /v3/routes/{guid}/destinations` with `port` field). go-cfclient supports this fully: `InsertDestinations()`, `ReplaceDestinations()`, `RemoveDestination()`.
- **Non-routable container ports** (what the scraper needs) have no V3 API equivalent. The V2 `ports` array was the only way to tell Diego "expose port 2112 on the container network without routing it through gorouter."

**Migration options for non-routable ports:**

1. **Internal route + destination:** Create a route on an internal domain (`apps.internal`), then add a destination targeting the metrics port. The scraper would reach the app via `app-name.apps.internal:2112`. This works but changes the scraper's connectivity model — it must resolve the internal route instead of using the container IP directly.

2. **Process health-check port:** If the metrics port is also the health-check port, the process configuration already exposes it. But metric-registrar uses a dedicated metrics port separate from the app's health check.

3. **Sidecar process:** Declare the metrics endpoint as a sidecar process in the app manifest. This is architecturally clean but requires manifest changes rather than API calls.

4. **Keep using `cf curl`:** Continue calling the V2 endpoints for port management until they are removed. This buys time but doesn't solve the problem.

**Recommendation for metric-registrar:** Option 1 (internal route + destination) is the most viable V3 migration path. It requires changing the scraper's connectivity model from "container IP + port" to "internal route hostname + port," which is a platform-side change — not just a plugin change. This confirms that the port migration is a **cross-component redesign** requiring coordination between the plugin and the metric-registrar platform component.

**Impact on the transitional migration approach:** The port migration cannot be handled by the V2Compat wrapper or the code generator. It is explicitly a "category 2: redesign" migration (see below) that requires human judgment and cross-component coordination. The generated wrapper approach correctly surfaces this as work the developer must handle manually.

**Phase 2 result:**

| Component | Migration Type | Effort |
|---|---|---|
| `registrations/fetcher.go` — UPS listing | Clean V3 substitution | Low — swap URL patterns and JSON paths |
| `registrations/fetcher.go` — binding lookup | Clean V3 substitution | Low — `relationships.app.data.guid` |
| `ports/ports.go` — read ports | Structural redesign | **High** — flat array → per-route destinations |
| `ports/ports.go` — write ports | Structural redesign | **High** — app-centric → route-centric |
| CLI command delegation | Optional future work | Low — works as-is |

**Key insight:** The metric-registrar migration reveals two distinct categories of V2→V3 work:

1. **Substitution** — Same concept, different API shape. Domain methods (`GetApp`, `GetApps`, `GetServices`) and flat V2 endpoints (UPS listing, binding lookup) map to V3 equivalents with minor field-path changes. The generated wrapper approach handles this well.

2. **Redesign** — The V3 model is fundamentally different. App ports moving to route destinations is not a field rename — it's a different domain model. No generator can bridge this; the plugin developer must understand the V3 model and rewrite the affected code.

The transitional approach handles category 1 automatically and surfaces category 2 as the work that requires human judgment.

##### mysql-cli-plugin (Heavy CliCommand Usage)

The mysql-cli-plugin demonstrates the scanner's value for `CliCommand`-heavy plugins. The scanner detected 14 `CliCommand` calls:

- `bind-service`, `create-service`, `delete`, `push`, `start`, `logs`, `rename-service`, `service-key`, `create-service-key`, `delete-service-key`, 2× `curl`, plus 1 dynamic command via variable
- Plus `GetService` domain method (fields: `LastOperation.State`, `LastOperation.Description`)

The scan output gives the developer a complete inventory of what needs attention, categorized by urgency: domain methods (must migrate), curl calls (should migrate), workflow commands (can stay).

### Companion Package Design

The companion package at `code.cloudfoundry.org/cli-plugin-helpers/cfclient` SHOULD be published to support the transitional migration:

```go
package cfhelper

import (
    "code.cloudfoundry.org/cli/plugin"
    "github.com/cloudfoundry/go-cfclient/v3/client"
    "github.com/cloudfoundry/go-cfclient/v3/config"
)

// CfConnection extends the standard plugin.CliConnection with direct V3 access.
type CfConnection interface {
    plugin.CliConnection
    CfClient() *client.Client
}

// NewCfConnection wraps an existing plugin.CliConnection and provides a
// go-cfclient V3 client constructed from the connection's credentials.
func NewCfConnection(conn plugin.CliConnection) (CfConnection, error) {
    endpoint, err := conn.ApiEndpoint()
    if err != nil {
        return nil, err
    }
    token, err := conn.AccessToken()
    if err != nil {
        return nil, err
    }
    skipSSL, err := conn.IsSSLDisabled()
    if err != nil {
        return nil, err
    }
    cfg, err := config.New(endpoint,
        config.Token(token),
        config.SkipTLSValidation(skipSSL),
    )
    if err != nil {
        return nil, err
    }
    cfClient, err := client.New(cfg)
    if err != nil {
        return nil, err
    }
    return &cfConnection{CliConnection: conn, client: cfClient}, nil
}

type cfConnection struct {
    plugin.CliConnection
    client *client.Client
}

func (c *cfConnection) CfClient() *client.Client {
    return c.client
}
```

Note: Unlike the Rabobank library, this companion package does **not** reimplement V2 domain methods via V3. Plugins SHOULD either use go-cfclient directly for domain operations, or use the generated V2 compatibility wrappers that populate only the fields a plugin declares it needs.

### Decoupling Internal Imports via `cf-plugin-helpers`

For the 7 internal packages rated trivial through medium in the [Function-Level Complexity Assessment](#function-level-complexity-assessment), standalone replacement packages with **matching function signatures** allow plugins to migrate by changing import paths only — no code changes required. The replacement functions are best-effort reimplementations, not exact behavioral clones. Only the function signatures need to match.

**Module:** `code.cloudfoundry.org/cf-plugin-helpers` (extends the existing [cli-plugin-helpers](https://github.com/cloudfoundry/cli-plugin-helpers) repository, which already provides `CliConnection` test doubles)

**Packages and signatures:**

```
cf-plugin-helpers/
├── cfconfig/        # replaces cf/configuration/confighelpers
├── cfformat/        # replaces cf/formatters
├── cftrace/         # replaces cf/trace
└── cfui/            # replaces cf/terminal
```

#### `cfconfig` — replaces `cf/configuration/confighelpers` (5 plugins)

```go
package cfconfig

// DefaultFilePath returns the path to the CF CLI config file.
// Checks $CF_HOME first, falls back to $HOME/.cf/config.json.
func DefaultFilePath() (string, error)

// PluginRepoDir is a function variable that returns the plugin repo directory.
// Checks $CF_PLUGIN_HOME first, falls back to the CF home directory.
var PluginRepoDir func() string
```

Implementation: ~30 lines of stdlib. Matches the original signatures exactly — `DefaultFilePath()` returns `(string, error)` and validates `$CF_HOME` exists; `PluginRepoDir` is a `var` (function variable), not a regular function, matching the original declaration in `confighelpers`.

#### `cfformat` — replaces `cf/formatters` (1 plugin)

```go
package cfformat

// ByteSize returns a human-readable byte size string (e.g., "1.5M", "256K").
func ByteSize(bytes int64) string
```

Implementation: ~15 lines of stdlib (`fmt.Sprintf` with unit thresholds).

#### `cftrace` — replaces `cf/trace` and provides V3 call tracing

Replaces `cf/trace` (6 plugins, but 4 are transitive via terminal) and provides HTTP tracing for V3 calls in generated wrappers.

```go
package cftrace

import "net/http"

// Printer is the interface for trace output.
type Printer interface {
    Print(v ...interface{})
    Printf(format string, v ...interface{})
    Println(v ...interface{})
    WritesToConsole() bool
}

// NewLogger returns a Printer that conditionally logs based on CF_TRACE.
// boolsOrPaths are checked in order: "true"/"false" toggle console output,
// any other string is treated as a file path for trace output.
func NewLogger(writer io.Writer, verbose bool, boolsOrPaths ...string) Printer

// NewWriterPrinter returns a Printer that writes to the given writer.
func NewWriterPrinter(writer io.Writer, writesToConsole bool) Printer

// NewTracingTransport wraps an http.RoundTripper to log HTTP requests and
// responses when CF_TRACE is enabled. Output format follows CF CLI conventions:
// REQUEST/RESPONSE blocks with timestamps, sorted headers, and
// [PRIVATE DATA HIDDEN] for Authorization headers.
//
// If base is nil, http.DefaultTransport is used. The caller is responsible
// for configuring TLS on the base transport before wrapping — go-cfclient's
// skipTLSValidation only recognizes *http.Transport and *oauth2.Transport,
// so the tracing transport must wrap an already-configured base.
func NewTracingTransport(base http.RoundTripper, logger Printer) http.RoundTripper
```

Implementation: ~120 lines total. The `Printer` interface and `NewWriterPrinter` are trivial. `NewLogger` checks `CF_TRACE` env var and conditionally writes to console or file. `NewTracingTransport` implements `http.RoundTripper`, dumps request/response when the logger is active, and sanitizes auth headers.

**Why V3 call tracing is required:** Before migration, a plugin calling `conn.GetApp("myapp")` triggers a host-side V2 API call that is visible in `CF_TRACE` output. After migration, the same logical operation becomes a guest-side V3 API call via go-cfclient — completely invisible to `CF_TRACE`. Without tracing, developers debugging issues with the transitional generated code have no way to see what HTTP requests the wrapper is making, what responses CAPI returns, or why a V3 call produces different results than the V2 call it replaced. This is especially critical during the migration period when developers are actively comparing V2 and V3 behavior to verify correctness.

**V3 call tracing in the generated wrapper:** The V2Compat wrapper constructs a go-cfclient `*client.Client` from the plugin's session credentials. When `CF_TRACE` is enabled, the generated code injects `NewTracingTransport` as the base transport via `config.HttpClient()`:

```go
// In generated V2Compat constructor
logger := cftrace.NewLogger(os.Stderr, false, os.Getenv("CF_TRACE"))
transport := cftrace.NewTracingTransport(http.DefaultTransport, logger)
httpClient := &http.Client{Transport: transport}

cfg, _ := config.New(endpoint,
    config.Token(token),
    config.SkipTLSValidation(skipSSL),
    config.HttpClient(httpClient),
)
```

This restores the debugging experience that would otherwise be lost when migrating from host-side V2 calls (visible to `CF_TRACE`) to guest-side V3 calls (invisible without this). Developers running `CF_TRACE=true cf my-plugin-command` will see both the host's RPC traffic and the guest's CAPI V3 traffic.

#### `cfui` — replaces `cf/terminal` Pattern B (5 plugins)

```go
package cfui

import "code.cloudfoundry.org/cf-plugin-helpers/cftrace"

// UI provides formatted terminal output for CF CLI plugins.
type UI interface {
    Say(message string, args ...interface{})
    Warn(message string, args ...interface{})
    Failed(message string, args ...interface{})
    Ok()
    Table(headers []string) UITable
}

// UITable supports row-based table output.
type UITable interface {
    Add(row ...string)
    Print()
}

// TeePrinter captures output while also printing it.
type TeePrinter struct { /* ... */ }

func NewTeePrinter(w io.Writer) *TeePrinter

// NewUI creates a UI instance.
func NewUI(in io.Reader, out io.Writer, printer *TeePrinter, logger cftrace.Printer) UI

// Color functions for formatted output.
func EntityNameColor(message string) string
func CommandColor(message string) string
func FailureColor(message string) string

// InitColorSupport initializes color support based on CF_COLOR env var.
var UserAskedForColors string
func InitColorSupport()
```

Implementation: ~100 lines. Tables via `text/tabwriter`, color via `fatih/color` (or ANSI escapes directly), `Say`/`Warn`/`Failed` via `fmt.Fprintf` with color wrapping.

#### Migration Example

**app-autoscaler-cli-plugin** — validated against upstream commit `641a1a8` (pre-V3 changes). 4 import lines changed across 4 files, zero code changes, all tests pass.

Because the replacement package names differ from the originals (`cftrace` vs `trace`, `cfconfig` vs `confighelpers`), import aliases preserve all existing code references:

```go
// Before (imports from CLI internals)
import (
    "code.cloudfoundry.org/cli/cf/trace"
    "code.cloudfoundry.org/cli/cf/configuration/confighelpers"
)

// After (import aliases — zero code changes, all trace.X and confighelpers.X references work)
import (
    trace "code.cloudfoundry.org/cf-plugin-helpers/cftrace"
    confighelpers "code.cloudfoundry.org/cf-plugin-helpers/cfconfig"
)
```

#### What Is NOT Covered by `cf-plugin-helpers`

| Package | Why Not | Guidance |
|---|---|---|
| `cf/terminal` Pattern A (multiapps) | 16 files, full UI framework — too deep for a generic helper | Three options: (a) copy `cf/terminal` into the plugin — but it pulls in `cf/trace`, `cf/i18n`, and internal types, (b) keep existing import — works while frozen, (c) request CLI team extract `cf/terminal` into a standalone module. The `cfui` package covers Pattern B basics; multiapps' `EntityNameColor` (30+ calls), `CommandColor`, `FailureColor`, and full `UI` interface require the complete package. CLI team is not bound to maintain compatibility. |
| `cf/flags` | 1 plugin, 1 struct field — stdlib `flag` or `pflag` is the natural replacement | Scanner should suggest `flag` (stdlib) |
| `cf/configuration` + `coreconfig` | Reads/writes undocumented config file — no stable contract exists | Four options: (a) copy code into plugin, (b) keep existing import, (c) request CLI team provide supported integration, (d) incorporate targets functionality into the CLI itself. All carry risk — see [analysis in Problem section](#function-level-complexity-assessment). CLI team is not bound to maintain compatibility. |
| `util/configv3`, `util/ui` | mysql-cli-plugin only; `configv3` usage is a transitive dep of `util/ui`; actual need is one confirmation prompt | Replace `ui.NewUI().DisplayBoolPrompt()` with `fmt.Print` + `bufio.Scanner` |

### Lessons from the Rabobank Implementation

The Rabobank `cf-plugins` library validates this approach but also reveals pitfalls to avoid.

#### What Worked

- **Zero host changes.** The library wraps `plugin.CliConnection` and calls `plugin.Start()` — compatible with any CF CLI version.
- **Incremental adoption.** Consumer plugins adopt at their own pace — 2 of 4 Rabobank consumers use the library.
- **Two migration tiers.** `plugins.Start()` provides transparent V3 reimplementation; `plugins.Execute()` provides direct `CfClient()` access.

#### Consumer Plugin Analysis: Was the Full Reimplementation Necessary?

The `cf-plugins` library reimplements **10 V2 domain methods** via V3. To evaluate whether this scope was justified, we analyzed both the current state and the git history of all 4 consumer plugins.

**Historical V2 Domain Method Usage:**

| Plugin | V2 Methods Used Historically | How They Migrated | Key Commits |
|---|---|---|---|
| scheduler-plugin | `GetServices()`, `GetService()`, `GetApp()` | Migrated directly to go-cfclient/v3 — did **not** adopt cf-plugins | [`57130bdb`](https://github.com/rabobank/scheduler-plugin/commit/57130bdb), [`e682b800`](https://github.com/rabobank/scheduler-plugin/commit/e682b800) |
| credhub-plugin | `GetService()` | Adopted cf-plugins — **2-line change** in `main.go` | [`e5355478`](https://github.com/rabobank/credhub-plugin/commit/e5355478), [`7cdaded9`](https://github.com/rabobank/credhub-plugin/commit/7cdaded9) |
| npsb-plugin | None (context methods only, from day one) | No migration needed | [`ef0c3e10`](https://github.com/rabobank/npsb-plugin/commit/ef0c3e10) |
| idb-plugin | None (built with cf-plugins from the start, Oct 2025) | Born V3-native via `CfClient()` | [`938005ae`](https://github.com/rabobank/idb-plugin/commit/938005ae) |

The cf-plugins library was created **Oct 2, 2025** ([`a0486ef6`](https://github.com/rabobank/cf-plugins/commit/a0486ef6)). The scheduler-plugin's migration commit message is explicit: *"remove dependency on some cliConnection calls since they still require cf v2 api"*. Commit authorship confirms the same developer migrated the scheduler-plugin and then created the cf-plugins library, generalizing the pattern for other consumers.

**The library solved a real problem.** The credhub-plugin migration demonstrates the library's value: a 2-line change (`plugin.Start()` → `plugins.Start()`) transparently replaced V2 RPC-backed domain method calls with V3 API calls.

**But the scope was broader than needed.** Across all 4 consumer plugins, historically and currently, only **3 of 10** reimplemented methods were ever called:

| Reimplemented Method | Ever Called By | Fields Accessed |
|---|---|---|
| `GetApp()` | scheduler-plugin (historical, now migrated away) | `.Guid` only |
| `GetService()` | credhub-plugin (current), scheduler-plugin (historical) | `.Guid`, `.ServiceOffering.Name`, `.LastOperation.State` |
| `GetServices()` | scheduler-plugin (historical, now migrated away) | `.Name`, `.ServiceOffering.Name` |
| `GetApps()` | Never called | — |
| `GetOrg()` | Never called | — |
| `GetOrgs()` | Never called | — |
| `GetSpace()` | Never called | — |
| `GetSpaces()` | Never called | — |
| `GetOrgUsers()` | Never called | — |
| `GetSpaceUsers()` | Never called | — |

The library reimplemented 10 methods, but only 3 were ever used — and those 3 accessed a combined total of **5 unique fields**. The remaining 7 reimplementations were speculative.

**Current State (Post-Migration):**

| Plugin | Uses cf-plugins? | V2 Domain Methods Called Now | Fields Accessed Now |
|---|---|---|---|
| scheduler-plugin | **No** | None | N/A — uses `go-cfclient/v3` directly |
| npsb-plugin | **No** | None | N/A — uses direct HTTP with `AccessToken()` |
| idb-plugin | Yes (`Execute()`) | None | N/A — uses `CfClient()` for V3 access |
| credhub-plugin | Yes (`Start()`) | **`GetService()` only** | `.Guid`, `.ServiceOffering.Name`, `.LastOperation.State` |

Today, only 1 method is called by 1 plugin, accessing 3 fields.

**Takeaway:** The cf-plugins library was a valid response to a real migration pressure, and the `plugins.Start()` pattern (transparent drop-in) was an elegant design. But the all-or-nothing reimplementation strategy — building every V2 method before knowing which ones consumers need — is exactly the waste the generated wrapper approach avoids. Build only what you use.

#### What the Generated Approach Improves

| Metric | Rabobank Library | Generated Wrapper |
|---|---|---|
| Methods reimplemented | 10 (all) | Only those declared in YAML |
| API calls for `GetApp` | 10+ (all fields) | 1–10 (only declared field groups) |
| API calls for `GetService` | 3 (instance, plan, offering) | **1** (using `fields` parameter) |
| API calls for `GetServices` (N instances) | 1 + 3×N | **2** (using `fields` + `include`) |
| Lines of wrapper code | ~500 | ~40–200 (varies by config) |

The most significant optimization is for service-related methods: the generator uses CAPI V3 `fields[service_plan]` and `fields[service_plan.service_offering]` parameters to retrieve instance, plan, and offering data in a **single API call**, eliminating the per-instance GET requests that Rabobank makes.

#### Rabobank Caveats in Context

The Rabobank README lists several caveats. Some are **not actually limitations** — they faithfully represent what V2 always provided:

| Rabobank Caveat | Actually a problem? | Explanation |
|---|---|---|
| Single buildpack only | **No.** | V2's `BuildpackUrl` was always a single string. The wrapper correctly returns the first buildpack. |
| Single process type | **No.** | V2 had no concept of multiple process types. The wrapper populates from the `web` process, matching V2 behavior. |
| `IsAdmin` always false | **Avoidable.** | Rabobank skipped the UAA role query. The generated wrapper includes it only if declared. |
| No per-app stats in list | **Avoidable.** | Rabobank omitted stats to avoid N+1 per-process calls. The generated wrapper includes stats calls only if the plugin declares it needs `RunningInstances`. |
| 11 API calls for `GetApp()` | **Avoidable.** | Rabobank populates every field. The generated wrapper makes only the calls needed for declared fields. |

#### Implementation Bugs to Avoid

1. **Do not strip the token prefix manually.** Rabobank uses `token[7:]` to strip `"bearer "`. go-cfclient's `config.Token()` handles this internally.
2. **Do not hardcode SSL settings.** Pass through the host's `IsSSLDisabled()` value.
3. **Do not hardcode user agent strings.** Derive from the plugin's metadata.

### go-cfclient V3 Guidance

#### Library Status

go-cfclient v3 is published at `github.com/cloudfoundry/go-cfclient/v3`. As of March 2026, the latest release is **v3.0.0-alpha.20**. Despite the alpha label, the library is in production use by multiple CF CLI plugins and has near-complete CAPI V3 coverage.

#### Versions in Production Use

Surveyed plugins pin to different alpha versions:

| Plugin | go-cfclient Version | Date |
|---|---|---|
| cf-lookup-route | v3.0.0-alpha.9 | 2024 |
| Rabobank cf-plugins | v3.0.0-alpha.15 | 2025 |
| DefaultEnv | v3.0.0-alpha.17 | 2025 |
| App Autoscaler | v3.0.0-alpha.19 | 2026 |

The spread from alpha.9 to alpha.19 indicates breaking changes between versions that forced plugins to pin rather than track latest.

#### Minimum Version Recommendation

Plugins SHOULD use **v3.0.0-alpha.17 or later**. This version:
- Includes `config.Token()` that handles the `"bearer "` prefix internally
- Supports `config.SkipTLSValidation()` with a boolean parameter
- Has stable interfaces for all resources used by the generated wrappers

Plugins SHOULD pin to a specific alpha version in `go.mod` and upgrade deliberately. Once go-cfclient releases v3.0.0 stable, all plugins SHOULD upgrade to it.

#### CAPI V3 Coverage

go-cfclient v3 implements **31 of 35 CAPI V3 resource groups with full coverage**. Every resource needed for plugin migration is fully supported. The library adds value beyond raw coverage:
- **Pagination:** `ListAll` (auto-pages), `First`, `Single` helpers
- **Include-based eager loading:** e.g., list apps with their spaces and orgs in one call
- **Async job polling:** `PollComplete`, `PollStaged`, `PollReady`
- **Typed filters:** `AppListOptions`, `ServiceInstanceListOptions`, etc.

#### CF API Version Floor

go-cfclient v3 requires CAPI V3 endpoints. The minimum depends on which resources the plugin uses:

| Resource | Minimum CAPI Version | CF Deployment |
|---|---|---|
| Apps, Spaces, Orgs (core) | 3.0.0 | 1.0+ |
| Service Instances (user-provided) | 3.0.0 | 1.0+ |
| Service Credential Bindings | 3.77.0 | ~18.0+ |
| Route Destinations | 3.77.0 | ~18.0+ |
| Service Plans, Offerings | 3.77.0 | ~18.0+ |

Most actively maintained CF foundations run CAPI 3.100+ (CF Deployment 25+), so the version floor is not a practical concern. The generated wrappers SHOULD document the minimum CAPI version per V3 resource used.

#### Token Lifecycle

`AccessToken()` returns a *snapshot* — the token value at the moment the RPC call is made. The host manages token refresh internally (via `config.ConfigRepository`), but go-cfclient does not receive a refresh token. This creates two patterns depending on plugin lifetime:

**Short-lived plugins** (most plugins — run a command and exit):

Use `config.Token()` directly. The token returned by `AccessToken()` at startup is valid for the plugin's entire lifetime (CF tokens typically last 10–20 minutes; most plugin commands complete in seconds).

```go
cfg, _ := config.New(endpoint,
    config.Token(token),           // snapshot token — valid for short operations
    config.SkipTLSValidation(skipSSL),
)
```

**Long-running plugins** (polling loops, watch commands, streaming):

Use `config.TokenProvider()` to re-fetch from the host's RPC on each API call. The host refreshes the token transparently via its own OAuth2 flow, so each call to `AccessToken()` returns a current token.

```go
cfg, _ := config.New(endpoint,
    config.TokenProvider(func() (string, error) {
        return conn.AccessToken() // re-fetches from host RPC each time
    }),
    config.SkipTLSValidation(skipSSL),
)
```

**When `TokenProvider` is necessary:** If a plugin makes API calls over a span longer than the token's TTL (typically 10–20 minutes), a snapshot token will expire mid-operation. Symptoms: go-cfclient returns 401 errors after the plugin has been running for several minutes. The fix is switching from `config.Token()` to `config.TokenProvider()`.

**Edge case:** If the host (CF CLI) exits while the plugin is still running, `conn.AccessToken()` will return an RPC error. Long-running plugins SHOULD handle this gracefully — see Error Handling at the Boundary.

The generated V2Compat wrapper uses `config.Token()` by default (appropriate for the common case). Plugins that need long-running behavior SHOULD switch to `config.TokenProvider()` manually — the generator does not make this choice automatically because it cannot determine plugin lifetime from static analysis.

### UAA and CredHub

The CAPI V2→V3 transition does not affect UAA or CredHub interfaces. Only 1 of 18 surveyed plugins calls UAA directly (a `client_credentials` token exchange). Only 1 plugin talks to CredHub (via a CAPI-resolved broker URL). The core contract's `AccessToken()` covers plugin needs.

### Error Handling at the Boundary

| Failure | Source | Recommended Handling |
|---|---|---|
| Empty API endpoint | `ApiEndpoint()` returns `""` | Fail fast: `"no API target — run 'cf api' first"` |
| Empty access token | `AccessToken()` returns `""` | Fail fast: `"not logged in — run 'cf login' first"` |
| Token expired during V3 call | go-cfclient returns 401 | Use `config.TokenProvider()` to re-fetch from host |
| V3 endpoint not available | go-cfclient returns 404 | Report minimum CAPI version required |
| Host RPC disconnected | `CliConnection` method returns error | Fail with context: the host (CLI) may have exited |

### Backward Compatibility

- **CLI version compatibility.** The transitional approach works with any CF CLI that implements `plugin.CliConnection` (v6.16+, v7, v8, v9). No host changes are required.
- **CAPI version compatibility.** go-cfclient/v3 requires CAPI V3 endpoints. Foundations running CF API v3.x (CF Deployment 1.x+) are supported. The generated wrappers SHOULD document the minimum CAPI version per V3 resource used.

### Build System Integration

- **`//go:generate` directive** for regeneration: `//go:generate cf-plugin-migrate generate`
- **Makefile target** (e.g., `make generate`) for Make-based builds
- **Generated file** SHOULD be checked into version control so `go install` works without the generator tool
- **Dependency management.** Adding go-cfclient/v3 pulls in `golang.org/x/oauth2`, `google.golang.org/protobuf`, and several transitive dependencies. For plugins that vendor dependencies, this increases the vendor directory. For plugins using module proxies, the impact is minimal.

### Test Migration

- **Existing mocks survive.** Tests that mock `plugin.CliConnection` continue to work for context methods (`AccessToken`, `GetCurrentSpace`, etc.).
- **New domain tests mock go-cfclient.** Plugins that switch to go-cfclient V3 for domain operations need either a `*httptest.Server` or a mock client. go-cfclient provides `fake.Client` in its test package.
- **Generated wrapper tests.** The generator SHOULD produce a companion `_test.go` file with table-driven tests that verify correct field population against a test server.

### Proof-of-Concept Candidates

To validate the transitional approach, three plugins from the [plugin survey](plugin-survey.md) represent increasing levels of migration complexity:

#### Tier 1: Simple — list-services

- **V2 methods used:** `GetApp()` — for GUID resolution only
- **Fields consumed:** `Guid` only (1 field → 1 V3 API call)
- **Additional patterns:** `cf curl` against V3 endpoint (already V3!), CLI internal imports (`cf/terminal`, `cf/trace`)
- **Migration:** Replace one `GetApp` call, switch `cf curl` to go-cfclient, replace `cf/terminal` with `text/tabwriter`
- **Why:** Demonstrates all three coupling patterns (V2 domain, cf curl, internal imports) in their simplest form. Already 90% V3. See [worked example above](#list-services-tier-1-simplest-domain-method-migration).

#### Tier 2: Moderate — OCF Scheduler

- **V2 methods used:** `GetApp()`, `GetApps()`
- **Fields consumed:** `Name`, `Guid`, `State` (3 fields → 1 V3 API call each)
- **Migration:** Two methods to replace, both using only core app fields available from a single `Applications` call
- **Why:** Actively maintained, representative of the common pattern. Already uses direct HTTP for scheduler operations — only the app lookup needs migration.

#### Tier 3: Complex — metric-registrar

- **V2 methods used:** `GetApp()`, `GetApps()`, `GetServices()`
- **Additional V2 dependency:** `cf curl /v2/user_provided_service_instances`, `/v2/apps/{guid}`
- **Fields consumed:** Multiple fields across app and service models
- **Migration:** Requires both generated wrappers (for V2 model methods) and `cf curl` replacement (for V2 CAPI endpoints). Tests the full migration path.
- **Why:** Most V2-coupled active plugin. If the transitional approach works here, it works everywhere.

#### Secondary Candidates

| Plugin | V2 Methods | Notes |
|---|---|---|
| spring-cloud-services | `GetService()`, `GetApps()` | Clean architecture, good test of service model migration |
| stack-auditor | `GetOrgs()` + `cf curl /v2/...` | Tests migration of both model methods and V2 curl calls |
| service-instance-logs | `GetService()` | V2 chain traversal (service → plan → service offering) — complex V3 mapping |

### Future Work: CAPI OpenAPI and Polyglot Clients

CAPI V3 has **no official OpenAPI specification**. All existing client libraries (Go, Java, Python) are hand-written. The community [capi-openapi-spec](https://github.com/cloudfoundry-community/capi-openapi-spec) project is the most promising path toward a machine-readable spec, which would enable auto-generated clients in any language.

Available CAPI V3 client libraries:

| Library | Language | Status |
|---|---|---|
| [go-cfclient](https://github.com/cloudfoundry/go-cfclient) | Go | Official, recommended |
| [cf-java-client](https://github.com/cloudfoundry/cf-java-client) | Java | Official |
| [cf-python-client](https://github.com/cloudfoundry-community/cf-python-client) | Python | Community |

## Technical Reference

This section provides the detailed data tables used by the `cf-plugin-migrate` tool. It is primarily for tool developers and contributors.

### YAML Schema: `cf-plugin-migrate.yml`

The YAML configuration declares which V2 domain methods the plugin calls and which fields it accesses. The generator uses this to produce minimal wrappers with only the V3 API calls required.

**Formal schema (version 1):**

```yaml
# Required: schema version for forward compatibility
schema_version: 1

# Go package name for the generated file (default: "main")
package: main

# V2 domain methods used by the plugin.
# Keys are method names from plugin.CliConnection.
# Only methods that return plugin_models.* types are supported.
methods:
  GetApp:
    # Top-level fields accessed on GetAppModel
    fields: [Guid, Name, State, Routes, Stack]
    # Sub-fields for composite types (required when parent is listed)
    # Convention: lowercase parent field name + "_fields"
    route_fields: [Host, Domain.Name, Port, Path]
    stack_fields: [Name, Guid, Description]
  GetApps:
    fields: [Guid, Name, State]
  GetServices:
    fields: [Guid, Name, ServicePlan, IsUserProvided]
    service_plan_fields: [Name, Guid]
  GetService:
    fields: [Guid, Name, DashboardUrl, ServicePlan, ServiceOffering, LastOperation]
    service_plan_fields: [Name, Guid]
    service_offering_fields: [Name, DocumentationUrl]
    last_operation_fields: [Type, State, Description]
  GetOrg:
    fields: [Guid, Name, Spaces, Domains, QuotaDefinition]
  GetOrgs:
    fields: [Guid, Name]
  GetSpace:
    fields: [Guid, Name, Organization, Applications, ServiceInstances]
  GetSpaces:
    fields: [Guid, Name]
  GetOrgUsers:
    fields: [Guid, Username, Roles]
  GetSpaceUsers:
    fields: [Guid, Username, Roles]

# CliCommand calls detected by scanner (informational — not consumed by generator)
cli_commands:
  - file: main.go
    line: 92
    method: CliCommandWithoutTerminalOutput
    command: curl
    endpoint: v2/apps
    v3_endpoint: /v3/apps
```

**Schema rules:**

| Rule | Description |
|---|---|
| `schema_version` | Required. Currently `1`. Allows the tool to evolve the schema without breaking existing configs. |
| `package` | Go package name for the generated `v2compat_generated.go`. Defaults to `main`. |
| `methods` | Map of V2 method name → field specification. Only methods listed are generated. |
| `fields` | List of top-level field names from the corresponding `plugin_models.*` struct. Must match the Go field name exactly. |
| `*_fields` | Sub-field specifiers for composite fields. Named `{lowercase_parent}_fields`. Dot notation for nested access (e.g., `Domain.Name`). Only required when the parent field is listed in `fields`. |
| `cli_commands` | Scanner output for CliCommand calls. Informational — the generator does not consume this section. |

**Supported methods and sub-field keys:**

| Method | Return Type | Available Sub-field Keys |
|---|---|---|
| `GetApp` | `GetAppModel` | `route_fields`, `stack_fields`, `instance_fields`, `service_fields` |
| `GetApps` | `[]GetAppsModel` | `route_fields` |
| `GetService` | `GetService_Model` | `service_plan_fields`, `service_offering_fields`, `last_operation_fields` |
| `GetServices` | `[]GetServices_Model` | `service_plan_fields`, `service_fields`, `last_operation_fields` |
| `GetOrg` | `GetOrg_Model` | `space_fields`, `domain_fields`, `space_quota_fields`, `quota_fields` |
| `GetOrgs` | `[]GetOrgs_Model` | (none) |
| `GetSpace` | `GetSpace_Model` | `org_fields`, `app_fields`, `service_instance_fields`, `domain_fields`, `security_group_fields`, `space_quota_fields` |
| `GetSpaces` | `[]GetSpaces_Model` | (none) |
| `GetOrgUsers` | `[]GetOrgUsers_Model` | (none — flat struct) |
| `GetSpaceUsers` | `[]GetSpaceUsers_Model` | (none — flat struct) |

### V3 API `include` and `fields` Parameter Availability

The CAPI V3 API supports two mechanisms for retrieving related resources in a single call:

- **`include`** — returns full related resources in an `included` section of the response
- **`fields`** — returns selected fields of related resources (supports nested paths like `service_plan.service_offering`)

| Endpoint | `include` Values | `fields` Resources |
|---|---|---|
| `/v3/apps` | `space`, `org`, `space.organization` | — |
| `/v3/routes` | `domain`, `space`, `space.organization` | — |
| `/v3/spaces` | `org`, `organization` | — |
| `/v3/roles` | `user`, `organization`, `space` | — |
| `/v3/service_credential_bindings` | `app`, `service_instance` | — |
| `/v3/service_instances` | — | `service_plan`, `service_plan.service_offering`, `service_plan.service_offering.service_broker` |
| `/v3/service_plans` | `service_offering` | `service_offering.service_broker` |
| `/v3/service_offerings` | — | `service_broker` |

The generator uses `include` and `fields` where available to minimize API calls.

### Generator Optimization Summary

The `include` and `fields` parameters available in CAPI V3 significantly reduce API calls compared to Rabobank's approach:

| Method | Rabobank Calls | Generator Calls (all fields) | Key Optimization |
|---|---|---|---|
| `GetApp` | 10 | 10 | `include=domain` on routes (eliminates URL parsing) |
| `GetApps` | 1 (partial) | 1 + per-app | Per-app calls for process/stats fields; results scoped by user permissions |
| `GetService` | 3 | **1** | `fields[service_plan]` + `fields[service_plan.service_offering]` |
| `GetServices` | 1 + 3×N | **2** | `fields` on list + single bindings call with `include=app` |
| `GetOrg` | 5 | 5 | No `include`/`fields` available on these endpoints |
| `GetSpace` | 7 | **6** | `include=organization` on spaces (eliminates org GET) |
| `GetOrgUsers` | 2 | 2 | Both use `include=user` |
| `GetSpaceUsers` | 3 | 3 | Both use `include=user` |

The most dramatic improvement is for service-related methods: `GetService` drops from 3 calls to 1, and `GetServices` drops from 1 + 3×N calls to 2 calls regardless of instance count.

### Complete V2→V3 Field Mapping Reference

Fields are organized into **dependency groups** — sets of fields that share the same V3 API call. The generator adds a V3 call only when at least one field from that group is requested.

The mapping is derived from first-principles analysis of the [V2 plugin model types](https://github.com/cloudfoundry/cli/tree/main/plugin/models) and the [CAPI V3 API documentation](https://v3-apidocs.cloudfoundry.org), validated against the [Rabobank implementation](https://github.com/rabobank/cf-plugins/blob/main/connection.go) and tested against a live CAPI V3 endpoint (v3.180.0).

#### GetAppModel — `GetApp(appName string)`

The most complex model. Rabobank populates all fields with 10 V3 API calls. The generated approach selects only the groups needed.

| Group | V3 API Call(s) | V2 Fields | V3 Field Path | Notes |
|---|---|---|---|---|
| **1: App** | `Applications.Single(name, spaceGUID)` | `Guid` | `.GUID` | Always required — base call |
| | | `Name` | `.Name` | |
| | | `State` | `.State` | |
| | | `SpaceGuid` | (input parameter) | Passed as filter, not from response |
| **2: Process** | `Processes.ListForApp(appGUID)` + `Processes.Get(processGUID)` | `Command` | `.Command` | First process type only (V2 had single process) |
| | | `DetectedStartCommand` | `.Command` | V3 does not distinguish detected vs explicit |
| | | `DiskQuota` | `.DiskInMB` | |
| | | `InstanceCount` | `.Instances` | |
| | | `Memory` | `.MemoryInMB` | |
| | | `HealthCheckTimeout` | `.HealthCheck.Data.Timeout` | Pointer — nil means no timeout set |
| **3: Stats** | `Processes.GetStats(processGUID)` | `RunningInstances` | `len(.Stats)` | Requires Group 2 for process GUID |
| | | `Instances[].State` | `.Stats[].State` | |
| | | `Instances[].Details` | `.Stats[].Details` | Pointer |
| | | `Instances[].Since` | computed from `.Stats[].Uptime` | `time.Now() - uptime` |
| | | `Instances[].CpuUsage` | `.Stats[].Usage.CPU` | |
| | | `Instances[].DiskQuota` | `.Stats[].DiskQuota` | In bytes (not MB) |
| | | `Instances[].DiskUsage` | `.Stats[].Usage.Disk` | |
| | | `Instances[].MemQuota` | `.Stats[].MemoryQuota` | |
| | | `Instances[].MemUsage` | `.Stats[].Usage.Memory` | |
| **4: Droplet** | `Droplets.GetCurrentForApp(appGUID)` | `BuildpackUrl` | `.Buildpacks[0].Name` | V2 exposed single buildpack; V3 supports multiple |
| | | `PackageState` | `.State` | Droplet state, not package state |
| | | `StagingFailedReason` | `.Error` | Pointer |
| **5: Stack** | `Stacks.Single(droplet.Stack)` | `Stack.Guid` | `.GUID` | Requires Group 4 for stack name from droplet |
| | | `Stack.Name` | `.Name` | |
| | | `Stack.Description` | `.Description` | Pointer |
| **6: Package** | `Packages.ListForAppAll(appGUID)` | `PackageUpdatedAt` | last `.UpdatedAt` | Assumes latest package is last in list |
| **7: Env** | `Applications.GetEnvironmentVariables(appGUID)` | `EnvironmentVars` | (full map) | |
| **8: Routes** | `Routes.ListForApp(appGUID, include=domain)` | `Routes[].Guid` | `.GUID` | `include=domain` eliminates separate domain lookups |
| | | `Routes[].Host` | `.Host` | |
| | | `Routes[].Path` | `.Path` | |
| | | `Routes[].Port` | `.Port` | Pointer |
| | | `Routes[].Domain.Guid` | `.Relationships.Domain.Data.GUID` | |
| | | `Routes[].Domain.Name` | (included Domain).Name | Via `include=domain` |
| **9: Services** | `ServiceCredentialBindings.List(appGUID, include=service_instance)` | `Services[].Guid` | (included SI).GUID | `include=service_instance` avoids N+1 |
| | | `Services[].Name` | (included SI).Name | |

**Dependency chain:** Group 3 requires Group 2 (process GUID). Group 5 requires Group 4 (stack name from droplet). All other groups are independent.

#### GetAppsModel — `GetApps()`

| Group | V3 API Call(s) | V2 Fields | V3 Field Path | Notes |
|---|---|---|---|---|
| **1: Apps** | `Applications.ListAll(spaceGUID)` | `Guid` | `.GUID` | Always required |
| | | `Name` | `.Name` | |
| | | `State` | `.State` | |
| **2: Process** | `Processes.ListForApp()` **per app** | `TotalInstances` | `.Instances` | Per-app call |
| | | `Memory` | `.MemoryInMB` | |
| | | `DiskQuota` | `.DiskInMB` | |
| **3: Stats** | `Processes.GetStats()` **per app** | `RunningInstances` | `len(.Stats)` | Requires Group 2 |
| **4: Routes** | `Routes.ListForApp(include=domain)` **per app** | `Routes[].*` | (see GetAppModel Group 8) | Per-app call |

#### GetService_Model — `GetService(serviceName string)`

| Group | V3 API Call(s) | V2 Fields | V3 Field Path | Notes |
|---|---|---|---|---|
| **1: Instance+Plan+Offering** | `ServiceInstances.Single(name, spaceGUID)` with `fields[service_plan]` and `fields[service_plan.service_offering]` | `Guid` | `.GUID` | **Single call** — `fields` parameters include plan and offering |
| | | `Name` | `.Name` | |
| | | `DashboardUrl` | `.DashboardURL` | Pointer |
| | | `IsUserProvided` | `.Type == "user-provided"` | |
| | | `LastOperation.*` | `.LastOperation.*` | |
| | | `ServicePlan.Name` | (included plan).Name | Via `fields[service_plan]` |
| | | `ServicePlan.Guid` | (included plan).GUID | |
| | | `ServiceOffering.Name` | (included offering).Name | Via `fields[service_plan.service_offering]` |
| | | `ServiceOffering.DocumentationUrl` | (included offering).DocumentationURL | |

**Optimization:** Retrieves instance, plan, and offering in **1 API call** vs. Rabobank's 3.

#### GetServices_Model — `GetServices()`

| Group | V3 API Call(s) | V2 Fields | V3 Field Path | Notes |
|---|---|---|---|---|
| **1: Instances+Plans+Offerings** | `ServiceInstances.ListAll(spaceGUID)` with `fields[service_plan]` and `fields[service_plan.service_offering]` | `Guid`, `Name`, `IsUserProvided`, `LastOperation.*`, `ServicePlan.*`, `Service.Name` | Various | **Single call** with `fields` |
| **2: Apps** | `ServiceCredentialBindings.ListAll(include=app)` | `ApplicationNames` | (included App).Name | Single call with `include=app` |

**Optimization:** 2 calls regardless of instance count vs. Rabobank's 1 + 3×N.

#### GetOrg_Model — `GetOrg(orgName string)`

| Group | V3 API Call(s) | V2 Fields |
|---|---|---|
| **1: Org** | `Organizations.Single(name)` | `Guid`, `Name` |
| **2: Quota** | `OrganizationQuotas.Get(quotaGUID)` | `QuotaDefinition.*` |
| **3: Spaces** | `Spaces.ListAll(orgGUID)` | `Spaces[].Guid`, `Spaces[].Name` |
| **4: Domains** | `Domains.ListForOrganization(orgGUID)` | `Domains[].Guid`, `Domains[].Name`, `Domains[].OwningOrganizationGuid`, `Domains[].Shared` |
| **5: SpaceQuotas** | `SpaceQuotas.ListAll(orgGUID)` | `SpaceQuotas[].Guid`, `SpaceQuotas[].Name`, ... |

#### GetSpace_Model — `GetSpace(spaceName string)`

| Group | V3 API Call(s) | V2 Fields | Notes |
|---|---|---|---|
| **1: Space+Org** | `Spaces.Single(name, include=organization)` | `Guid`, `Name`, `Organization.*` | `include=organization` eliminates separate org GET |
| **2: Apps** | `Applications.ListAll(spaceGUID)` | `Applications[].Guid`, `Applications[].Name` | |
| **3: Services** | `ServiceInstances.ListAll(spaceGUID)` | `ServiceInstances[].Guid`, `ServiceInstances[].Name` | |
| **4: Domains** | `Domains.ListAll(orgGUID)` | `Domains[].*` | Org GUID from Group 1's included org |
| **5: SecurityGroups** | `SecurityGroups.ListAll(runningSpaceGUID)` | `SecurityGroups[].*` | |
| **6: SpaceQuota** | `SpaceQuotas.Get(quotaGUID)` | `SpaceQuota.*` | Only if quota assigned |

#### Simple Models

| Method | V3 API Call | V2 Fields |
|---|---|---|
| `GetOrgs()` | `Organizations.ListAll()` | `Guid`, `Name` |
| `GetSpaces()` | `Spaces.ListAll()` | `Guid`, `Name` |
| `GetOrgUsers(orgName)` | `Organizations.Single()` + `Roles.ListAll(include=user)` | `Guid`, `Username`, `Roles`, `IsAdmin` (always false) |
| `GetSpaceUsers(orgName, spaceName)` | `Organizations.Single()` + `Spaces.Single()` + `Roles.ListAll(include=user)` | `Guid`, `Username`, `Roles`, `IsAdmin` (always false) |

#### Fields Not Available in V3

| V2 Field | Model | Reason |
|---|---|---|
| `IsAdmin` | `GetOrgUsers_Model`, `GetSpaceUsers_Model` | V2 derived from UAA admin scope; V3 roles are CAPI-only |
| `DetectedStartCommand` (distinct from `Command`) | `GetAppModel` | V3 does not distinguish detected vs explicit start command |

### V2 Plugin Model Struct Reference

Complete Go type definitions from [`plugin/models/`](https://github.com/cloudfoundry/cli/tree/main/plugin/models) in the CF CLI source.

#### App Models

```go
// GetAppModel — returned by GetApp(appName string)
type GetAppModel struct {
    Guid                 string
    Name                 string
    BuildpackUrl         string
    Command              string
    DetectedStartCommand string
    DiskQuota            int64 // in Megabytes
    EnvironmentVars      map[string]interface{}
    InstanceCount        int
    Memory               int64 // in Megabytes
    RunningInstances     int
    HealthCheckTimeout   int
    State                string
    SpaceGuid            string
    PackageUpdatedAt     *time.Time
    PackageState         string
    StagingFailedReason  string
    Stack                *GetApp_Stack
    Instances            []GetApp_AppInstanceFields
    Routes               []GetApp_RouteSummary
    Services             []GetApp_ServiceSummary
}

type GetApp_Stack struct {
    Guid, Name, Description string
}

type GetApp_AppInstanceFields struct {
    State     string
    Details   string
    Since     time.Time
    CpuUsage  float64 // percentage
    DiskQuota int64   // in bytes
    DiskUsage int64
    MemQuota  int64
    MemUsage  int64
}

type GetApp_RouteSummary struct {
    Guid   string
    Host   string
    Domain GetApp_DomainFields
    Path   string
    Port   int
}

type GetApp_DomainFields struct {
    Guid, Name string
}

type GetApp_ServiceSummary struct {
    Guid, Name string
}

// GetAppsModel — returned by GetApps()
type GetAppsModel struct {
    Name             string
    Guid             string
    State            string
    TotalInstances   int
    RunningInstances int
    Memory           int64
    DiskQuota        int64
    Routes           []GetAppsRouteSummary
}

type GetAppsRouteSummary struct {
    Guid   string
    Host   string
    Domain GetAppsDomainFields
}

type GetAppsDomainFields struct {
    Guid                   string
    Name                   string
    OwningOrganizationGuid string
    Shared                 bool
}
```

#### Service Models

```go
// GetService_Model — returned by GetService(serviceName string)
type GetService_Model struct {
    Guid            string
    Name            string
    DashboardUrl    string
    IsUserProvided  bool
    ServiceOffering GetService_ServiceFields
    ServicePlan     GetService_ServicePlan
    LastOperation   GetService_LastOperation
}

type GetService_LastOperation struct {
    Type, State, Description string
    CreatedAt, UpdatedAt     string
}

type GetService_ServicePlan struct {
    Name, Guid string
}

type GetService_ServiceFields struct {
    Name, DocumentationUrl string
}

// GetServices_Model — returned by GetServices()
type GetServices_Model struct {
    Guid             string
    Name             string
    ServicePlan      GetServices_ServicePlan
    Service          GetServices_ServiceFields
    LastOperation    GetServices_LastOperation
    ApplicationNames []string
    IsUserProvided   bool
}

type GetServices_LastOperation struct {
    Type, State string
}

type GetServices_ServicePlan struct {
    Guid, Name string
}

type GetServices_ServiceFields struct {
    Name string
}
```

#### Org Models

```go
// GetOrg_Model — returned by GetOrg(orgName string)
type GetOrg_Model struct {
    Guid            string
    Name            string
    QuotaDefinition QuotaFields
    Spaces          []GetOrg_Space
    Domains         []GetOrg_Domains
    SpaceQuotas     []GetOrg_SpaceQuota
}

type GetOrg_Space struct {
    Guid, Name string
}

type GetOrg_Domains struct {
    Guid                   string
    Name                   string
    OwningOrganizationGuid string
    Shared                 bool
}

type GetOrg_SpaceQuota struct {
    Guid                    string
    Name                    string
    MemoryLimit             int64
    InstanceMemoryLimit     int64
    RoutesLimit             int
    ServicesLimit           int
    NonBasicServicesAllowed bool
}

// Shared quota type used by GetOrg_Model and Organization
type QuotaFields struct {
    Guid                    string
    Name                    string
    MemoryLimit             int64
    InstanceMemoryLimit     int64
    RoutesLimit             int
    ServicesLimit           int
    NonBasicServicesAllowed bool
}

// GetOrgs_Model — returned by GetOrgs()
type GetOrgs_Model struct {
    Guid, Name string
}
```

#### Space Models

```go
// GetSpace_Model — returned by GetSpace(spaceName string)
type GetSpace_Model struct {
    GetSpaces_Model                          // embeds Guid, Name
    Organization     GetSpace_Orgs
    Applications     []GetSpace_Apps
    ServiceInstances []GetSpace_ServiceInstance
    Domains          []GetSpace_Domains
    SecurityGroups   []GetSpace_SecurityGroup
    SpaceQuota       GetSpace_SpaceQuota
}

type GetSpace_Orgs struct {
    Guid, Name string
}

type GetSpace_Apps struct {
    Name, Guid string
}

type GetSpace_ServiceInstance struct {
    Guid, Name string
}

type GetSpace_Domains struct {
    Guid                   string
    Name                   string
    OwningOrganizationGuid string
    Shared                 bool
}

type GetSpace_SecurityGroup struct {
    Name  string
    Guid  string
    Rules []map[string]interface{}
}

type GetSpace_SpaceQuota struct {
    Guid                    string
    Name                    string
    MemoryLimit             int64
    InstanceMemoryLimit     int64
    RoutesLimit             int
    ServicesLimit           int
    NonBasicServicesAllowed bool
}

// GetSpaces_Model — returned by GetSpaces()
type GetSpaces_Model struct {
    Guid, Name string
}
```

#### User Models

```go
// GetOrgUsers_Model — returned by GetOrgUsers(orgName string, args ...string)
type GetOrgUsers_Model struct {
    Guid     string
    Username string
    IsAdmin  bool     // Not available in V3 — always false
    Roles    []string
}

// GetSpaceUsers_Model — returned by GetSpaceUsers(orgName, spaceName string)
type GetSpaceUsers_Model struct {
    Guid     string
    Username string
    IsAdmin  bool     // Not available in V3 — always false
    Roles    []string
}
```

#### Context Models (Pass-through — No Wrappers Needed)

```go
// Organization — returned by GetCurrentOrg()
type Organization struct {
    OrganizationFields
}
type OrganizationFields struct {
    Guid            string
    Name            string
    QuotaDefinition QuotaFields
}

// Space — returned by GetCurrentSpace()
type Space struct {
    SpaceFields
}
type SpaceFields struct {
    Guid, Name string
}
```

These context models are populated by the host via RPC and pass through unchanged. They do not need generated wrappers.

## References

- [Plugin Survey](plugin-survey.md) — Survey of 18 actively maintained CF CLI plugins and Rabobank case study
- [cf-plugin-migrate Design](cf-plugin-migrate-design.md) — Detailed design decisions for the migration tool
- [Rabobank cf-plugins](https://github.com/rabobank/cf-plugins) — The production transitional wrapper library
- [CAPI V3 API Documentation](https://v3-apidocs.cloudfoundry.org) — Official Cloud Foundry V3 API reference
- [CAPI V2 API Documentation](https://v2-apidocs.cloudfoundry.org) — Legacy Cloud Foundry V2 API reference (deprecated)
- [CF CLI Plugin Models](https://github.com/cloudfoundry/cli/tree/main/plugin/models) — V2 plugin model type definitions (`plugin_models.*`)
- [go-cfclient](https://github.com/cloudfoundry/go-cfclient) — Cloud Foundry V3 Go client library
- [cf-java-client](https://github.com/cloudfoundry/cf-java-client) — Cloud Foundry Java client library
- [cf-python-client](https://github.com/cloudfoundry-community/cf-python-client) — Cloud Foundry Python client library
- [capi-openapi-spec](https://github.com/cloudfoundry-community/capi-openapi-spec) — Community-maintained OpenAPI spec for CAPI V3
- [cloud_controller_ng#2192](https://github.com/cloudfoundry/cloud_controller_ng/issues/2192) — Tracking issue for official CAPI OpenAPI spec
- [cloudfoundry/cli#3621](https://github.com/cloudfoundry/cli/issues/3621) — Plugin interface tracking issue
- [cloudfoundry/cli#3741](https://github.com/cloudfoundry/cli/pull/3741) — POC: host-side V3 rewrite of cli_rpc_server.go (discussion piece, not for merge)
- [CF Summit 2025: CF CLI Plugins — Current Status and Future](https://www.youtube.com/watch?v=MyYxHkeHvKo) — Norman Abramovitz & Al Berez ([transcript](cf-summit-2025-plugin-talk-transcript.md))
