# cf-plugin-migrate Tool — Design Decisions

## Overview

`cf-plugin-migrate` is a two-command CLI tool that helps plugin developers migrate from V2 domain methods to direct CAPI V3 access. It has two subcommands:

- **`scan`** — AST-based audit of Go source → produces `cf-plugin-migrate.yml`
- **`generate`** — reads YAML config → produces `v2compat_generated.go`

The tool generates minimal V2-compatible wrapper functions that return existing `plugin_models.*` types, populated by the minimum V3 API calls required for the fields the plugin actually uses.

This document records the design decisions made before implementation.

## Decision 1: Code Generation Approach

**Decision:** `text/template` + `go/format`

**Rationale:** The generated code follows a repeatable pattern — one function per V2 method, iterate over requested field groups, assign fields from V3 responses. This is the standard Go generator pattern used by `protoc-gen-go`, `stringer`, and similar tools. Templates make it easy for contributors to understand the output by reading the template directly.

`go/ast`-based code construction was considered but rejected. It is more verbose for straightforward code generation and makes it harder to see what the output looks like from reading the generator source. The generated wrappers are simple enough that type-safe AST construction adds complexity without proportional benefit.

`go/format` is applied to the template output to ensure consistent formatting regardless of template whitespace.

**Alternatives considered:**

| Approach | Why rejected |
|---|---|
| `go/ast` builder | Verbose for straightforward code; harder to reason about output from reading generator source |
| String concatenation | Fragile, poor readability, no structured template logic |

## Decision 2: Dependency Chain Handling

**Decision:** Hardcoded group ordering per method.

**Rationale:** The V2 plugin models are frozen — they will never change. The dependency chains are static, shallow (max depth 2), and cycle-free:

| Method | Dependency | Reason |
|---|---|---|
| `GetApp` | Stats → Process | Process GUID needed for `Processes.GetStats()` |
| `GetApp` | Stack → Droplet | Stack name from `Droplets.GetCurrentForApp()` needed for `Stacks.Single()` |
| `GetApps` | Stats → Process | Same as GetApp |
| `GetService` | Offering → Plan | Offering GUID from `ServicePlans.Get()` needed for `ServiceOfferings.Get()` |
| `GetServices` | Offering → Plan | Same as GetService |
| `GetSpace` | Domains → Org | Org GUID from `Spaces.Single()` needed for `Domains.ListAll()` |

Each method has a fixed ordered list of groups (e.g., `GetApp: [App, Process, Stats, Droplet, Stack, Package, Env, Routes, Services]`). The generator emits groups in this order, skipping groups with no requested fields. When a group is skipped, its dependents are also skipped (unless the dependent has an alternative source).

**Alternatives considered:**

| Approach | Why rejected |
|---|---|
| Topological sort | General-purpose dependency resolution is overkill for depth-2 static chains with no possibility of change |
| Implicit template ordering | Fragile; dependency relationships not visible in the template logic |

## Decision 3: Module Structure and Location

**Decision:** POC as a subdirectory in this RFC repository. Move to `cli-plugin-helpers` when the RFC is accepted.

**Rationale:** The proposed final location is `code.cloudfoundry.org/cli-plugin-helpers/cmd/cf-plugin-migrate`, but that requires CLI team coordination and repository access. Starting in the RFC repository keeps the tool co-located with the YAML schema definition, field mapping tables, and worked examples during development. This allows rapid iteration without external dependencies.

**Directory structure:**

```
cf-plugin-migrate/
  main.go                  # CLI entry point: scan / generate subcommands
  scanner/
    scanner.go             # go/ast + go/types analysis
  generator/
    generator.go           # template-based code generation
    templates/             # go:embed templates for generated functions
  mapping/
    mapping.go             # field-to-API-call dependency groups (Go data)
```

**Build targets:** Linux and Darwin, amd64 and arm64. Release builds use goreleaser with ldflags version injection. Local builds default to version `dev` with VCS metadata from `runtime/debug`.

**Dependencies:**

- `go/ast`, `go/types`, `go/parser` — for the scanner (stdlib, no external deps)
- `text/template`, `go/format` — for the generator (stdlib)
- `gopkg.in/yaml.v3` — for YAML parsing
- No dependency on go-cfclient or the CF CLI — the tool generates code that imports them, but does not import them itself

**Alternatives considered:**

| Approach | Why rejected |
|---|---|
| Directly in `cli-plugin-helpers` | Requires CLI team coordination before POC is validated |
| Standalone `cloudfoundry-community` repo | Extra repository management overhead for a POC |

## Decision 4: Generated Code Error Handling

**Decision:** Eager return with partial results.

**Rationale:** This matches the Rabobank pattern. The generated function builds up the result model progressively. Each V3 API call checks for errors and returns immediately on failure, but returns the partially-populated model rather than a zero value:

```go
func getApp(cfClient *client.Client, spaceGUID, name string) (plugin_models.GetAppModel, error) {
    var model plugin_models.GetAppModel

    // Group 1: App (always required)
    app, err := cfClient.Applications.Single(ctx, &client.AppListOptions{...})
    if err != nil {
        return model, err  // zero value — nothing populated yet
    }
    model.Guid = app.GUID
    model.Name = app.Name
    model.State = string(app.State)

    // Group 8: Routes (requested by developer)
    routes, err := cfClient.Routes.ListForApp(ctx, app.GUID, nil)
    if err != nil {
        return model, err  // partial result — Group 1 fields are valid
    }
    // ... populate model.Routes ...

    return model, nil
}
```

On failure, the caller receives all fields populated up to the point of failure. This is pragmatic — the base group (Group 1) always succeeds first, so the caller has at minimum the app's GUID, Name, and State.

**Alternatives considered:**

| Approach | Why rejected |
|---|---|
| Eager return, zero value | Discards already-fetched data; wasteful when later groups fail |
| Multi-error collection | Complex generated code; all independent groups would need concurrent execution to justify the complexity |

## Decision 5: Per-Item API Call Handling

**Decision:** Generate the code and note the per-item cost in YAML comments.

**Rationale:** Some V2 model fields require per-item API calls when used in list methods. For example, `GetApps` with `TotalInstances` requires `Processes.ListForApp()` for each app. The V2 CLI populated these from a summary endpoint that returned everything in one response — no V3 equivalent exists.

All V3 API list calls return only resources the user has permission to see. A space developer sees their space's apps; an org auditor sees their org's apps. The number of per-app calls matches the user's visibility scope — this is the normal behavior of the V3 API, not a special concern.

The generator produces the code and the YAML output from `scan` notes the additional calls:

```yaml
methods:
  GetApps:
    fields: [Guid, Name, State]
    # Additional calls per app: TotalInstances, RunningInstances, Memory, DiskQuota
```

If the developer keeps the fields, the generated code includes them. If they remove them, the generator omits those API calls entirely.

**Alternatives considered:**

| Approach | Why rejected |
|---|---|
| Warn and skip (refuse to generate) | Too restrictive; the developer may genuinely need these fields and the cost is bounded by user permissions |
| Generate concurrent calls (`errgroup`) | Adds significant complexity to generated code; harder to debug; sequential calls are adequate for the typical permission-scoped result set |

## Decision 6: Use V3 `include` and `fields` Parameters to Minimize API Calls

**Decision:** The generator MUST use `include` and `fields` query parameters where the CAPI V3 API supports them, collapsing multiple dependency groups into single calls.

**Rationale:** Testing against a live CAPI V3 endpoint (v3.180.0 at `https://api.sys.adepttech.ca`) revealed that several V3 endpoints support `include` (returns full related resources inline) and `fields` (returns selected fields of related resources, including nested paths). These parameters eliminate the need for per-item follow-up calls in many cases.

**Key optimizations discovered:**

| Method | Without `include`/`fields` | With `include`/`fields` | Savings |
|---|---|---|---|
| `GetService` | 3 calls (instance + plan GET + offering GET) | **1 call** with `fields[service_plan]` + `fields[service_plan.service_offering]` | 2 calls eliminated |
| `GetServices` | 1 + 3×N calls (per-instance plan + offering + bindings) | **2 calls** (list with `fields` + bindings with `include=app`) | 3×N calls → 2 |
| `GetSpace` | 7 calls (separate org GET) | **6 calls** with `include=organization` on spaces | 1 call eliminated |
| `GetApp` (Routes) | Routes + URL parsing for domain name | Routes with `include=domain` | Cleaner, no parsing |

**Available `include` and `fields` by endpoint (defined in the [CAPI V3 API documentation](https://v3-apidocs.cloudfoundry.org), verified against v3.180.0):**

| Endpoint | `include` | `fields` |
|---|---|---|
| `/v3/apps` | `space`, `org`, `space.organization` | — |
| `/v3/routes` | `domain`, `space`, `space.organization` | — |
| `/v3/spaces` | `org`, `organization` | — |
| `/v3/roles` | `user`, `organization`, `space` | — |
| `/v3/service_credential_bindings` | `app`, `service_instance` | — |
| `/v3/service_instances` | — | `service_plan`, `service_plan.service_offering`, `service_plan.service_offering.service_broker` |
| `/v3/service_plans` | `service_offering` | `service_offering.service_broker` |
| `/v3/service_offerings` | — | `service_broker` |
| `/v3/processes` | — | — |
| `/v3/domains` | — | — |
| `/v3/droplets` | — | — |
| `/v3/packages` | — | — |
| `/v3/stacks` | — | — |
| `/v3/security_groups` | — | — |
| `/v3/organization_quotas` | — | — |
| `/v3/space_quotas` | — | — |

**Impact on Decision 2 (dependency chains):** Some dependency chains are eliminated entirely. For example, `GetSpace` previously required a separate org GET (Group 2) before the domains lookup (Group 5) could use the org GUID. With `include=organization`, the org data comes back with the space in Group 1, removing the chain. Similarly, `GetService`'s plan → offering chain collapses into a single call.

**Impact on Decision 5 (per-item calls):** The `include` and `fields` parameters do not help with `GetApps` Groups 2–4 (processes and stats) because `/v3/processes` supports neither `include` nor `fields`. Per-app calls remain unavoidable for those fields. As with all V3 API calls, the results are scoped by the user's permissions.

**go-cfclient support:** The generator's output uses go-cfclient's typed API. Whether go-cfclient exposes `include` and `fields` as options on its list/get methods needs to be verified during implementation. If go-cfclient does not expose a particular parameter, the generator can fall back to raw HTTP calls or the previous multi-call approach.

## Background: V2 Plugin Registration and Execution Flow

The current V2 plugin architecture has two distinct phases: **install-time registration** and **run-time execution**. Both use the same RPC pair but for different purposes.

### Install-Time Registration

When a user runs `cf install-plugin <binary>`, the CLI needs to extract the plugin's metadata (name, version, commands) without trusting the binary to self-describe via stdout. The flow:

```
Host (CF CLI)                              Guest (plugin binary)
─────────────                              ────────────────────
1. Start RPC server on random port
2. exec.Command(path, port, "SendMetadata")
                                           3. plugin.Start(cmd) entry point
                                           4. NewCliConnection(os.Args[1])
                                           5. pingCLI() — TCP dial to confirm host is ready
                                           6. isMetadataRequest(os.Args) → true
                                           7. cmd.GetMetadata() → PluginMetadata
                                           8. RPC call: CliRpcCmd.SetPluginMetadata(metadata)
                                           9. os.Exit(0)
10. Read rpcService.RpcCmd.PluginMetadata
11. Convert to configv3.Plugin
12. Save to ~/.cf/config.json
13. Copy binary to ~/.cf/plugins/
```

The guest is executed solely to extract metadata. It calls `GetMetadata()`, sends the result over RPC, and exits. No `Run()` is called. No `CliConnection` methods are used.

**Key source files:**

| File (relative to `cloudfoundry/cli`) | Role |
|---|---|
| `plugin/plugin_shim.go` | Guest-side bootstrap — `plugin.Start(cmd)` handles both metadata and run paths |
| `command/plugin/shared/rpc.go` | Host-side `GetMetadata()` — starts RPC, execs with `"SendMetadata"`, reads result |
| `actor/pluginaction/install.go` | `GetAndValidatePlugin` — calls `GetMetadata`, validates version, checks for conflicts |

**The `plugin.Start()` function** (`plugin/plugin_shim.go`) is the universal entry point every plugin calls from `main()`:

```go
func Start(cmd Plugin) {
    cliConnection := NewCliConnection(os.Args[1])  // port
    cliConnection.pingCLI()
    if isMetadataRequest(os.Args) {
        cliConnection.sendPluginMetadataToCliServer(cmd.GetMetadata())
    } else {
        // check MinCliVersion, then:
        cmd.Run(cliConnection, os.Args[2:])
    }
}
```

### Run-Time Execution

When a user runs `cf <plugin-command> args...`, the CLI looks up the command in its saved plugin list and launches the plugin binary:

```
Host (CF CLI)                              Guest (plugin binary)
─────────────                              ────────────────────
1. Look up command in ~/.cf/config.json
2. Find plugin binary path (metadata.Location)
3. Start RPC server on random port
4. exec.Command(path, port, command, args...)
                                           5. plugin.Start(cmd) entry point
                                           6. NewCliConnection(os.Args[1])
                                           7. pingCLI()
                                           8. isMetadataRequest → false
                                           9. Check MinCliVersion (optional)
                                          10. cmd.Run(cliConnection, os.Args[2:])
                                          11. Plugin does its work, calling
                                              cliConnection methods as needed
                                          12. Plugin returns from Run()
13. Process exits (host kills if needed)
```

**Key source files:**

| File (relative to `cloudfoundry/cli`) | Role |
|---|---|
| `plugin/rpc/run_plugin.go` | `RunMethodIfExists` — matches command name/alias, starts RPC, execs plugin |
| `plugin/plugin_shim.go` | Guest-side — dispatches to `Run()` with `cliConnection` and args |

The plugin receives `os.Args` as `[port, command, arg1, arg2, ...]`. The `cliConnection` wraps the port and dials TCP for each RPC call. Every call — session, domain, or CLI passthrough — creates a new TCP connection via `withClientDo()`.

### What the Generated Package Preserves vs. Replaces

The registration handshake is **preserved unchanged**. The plugin still:
- Calls `plugin.Start(cmd)` from `main()`
- Implements `GetMetadata()` returning `plugin.PluginMetadata`
- Receives the RPC port and command args via `os.Args`
- Participates in the `SendMetadata` registration protocol

The generated package replaces what happens **after registration** — the `CliConnection` method implementations that the plugin calls during `Run()`. The developer's migration is an import change: replace the RPC-backed `CliConnection` with the generated standalone implementation that reads config directly and calls CAPI V3 for domain methods.



**Decision:** The generated package is a drop-in replacement for the V2 `CliConnection` RPC pair, implementing the full `plugin.CliConnection` interface without requiring the CLI host process.

**Rationale:** The current V2 plugin architecture uses a bidirectional RPC pair over TCP:

**Guest side** (`plugin/cli_connection.go`): The `cliConnection` struct holds only a `cliServerPort` string. Every method — session, domain, or CLI passthrough — dials a new TCP connection to `127.0.0.1:<port>`, calls the corresponding `CliRpcCmd.*` method via Go's `net/rpc` (gob encoding), and returns the result. The guest creates a new connection per call via `withClientDo()`.

**Host side** (`plugin/rpc/cli_rpc_server.go`): The `CliRpcCmd` struct holds two key dependencies:

| Field | Type | Purpose |
|---|---|---|
| `cliConfig` | `coreconfig.Repository` | Reads `~/.cf/config.json` — API endpoint, token, org/space targets, user info |
| `repoLocator` | `api.RepositoryLocator` | Factory for V2 CAPI repository objects (apps, services, orgs, etc.) |

The host-side methods fall into three categories:

**1. Session/context methods** — Read directly from `cliConfig`:

| Method | Implementation |
|---|---|
| `GetCurrentOrg` | `cliConfig.OrganizationFields().Name/GUID` |
| `GetCurrentSpace` | `cliConfig.SpaceFields().Name/GUID` |
| `Username` | `cliConfig.Username()` |
| `UserGuid` | `cliConfig.UserGUID()` |
| `UserEmail` | `cliConfig.UserEmail()` |
| `IsLoggedIn` | `cliConfig.IsLoggedIn()` |
| `IsSSLDisabled` | `cliConfig.IsSSLDisabled()` |
| `HasOrganization` | `cliConfig.HasOrganization()` |
| `HasSpace` | `cliConfig.HasSpace()` |
| `ApiEndpoint` | `cliConfig.APIEndpoint()` |
| `ApiVersion` | `cliConfig.APIVersion()` |
| `HasAPIEndpoint` | `cliConfig.HasAPIEndpoint()` |
| `DopplerEndpoint` | `cliConfig.DopplerEndpoint()` |
| `LoggregatorEndpoint` | Returns `""` (deprecated, hardcoded empty) |
| `AccessToken` | `repoLocator.GetAuthenticationRepository().RefreshAuthToken()` |

These are trivial config reads. `AccessToken` is the only one that does real work — it refreshes the OAuth token via the authentication repository.

**2. Domain methods** — All 10 follow the same pattern:

```go
func (cmd *CliRpcCmd) GetApp(appName string, retVal *plugin_models.GetAppModel) error {
    deps := commandregistry.NewDependency(...)
    deps.Config = cmd.cliConfig
    deps.RepoLocator = cmd.repoLocator
    deps.PluginModels.Application = retVal        // output pointer
    cmd.terminalOutputSwitch.DisableTerminalOutput(true)
    deps.UI = terminal.NewUI(...)
    return cmd.newCmdRunner.Command([]string{"app", appName}, deps, true)
}
```

Each domain method creates fresh CLI dependencies, sets the output model pointer in `deps.PluginModels`, disables terminal output, and runs an internal CLI command (e.g., `"app"`, `"apps"`, `"org"`, `"service"`). The command runner populates the model struct as a side effect. These internal commands use V2 CAPI endpoints via `repoLocator`.

**3. CLI passthrough methods** — `CliCommand` and `CliCommandWithoutTerminalOutput`:

The guest calls `callCliCommand(silently, args...)` which makes three sequential RPC calls within a single TCP connection:

1. `CliRpcCmd.DisableTerminalOutput(silently)` — toggles terminal output capture
2. `CliRpcCmd.CallCoreCommand(args)` — runs the CLI command via `commandregistry`
3. `CliRpcCmd.GetOutputAndReset()` — retrieves captured output lines

This is the "exec bypass" pattern — plugins run arbitrary CLI commands (e.g., `cf curl`, `cf create-service`) through the host process without shelling out.

**What the generated package does differently:**

| Category | Current (RPC to host) | Generated (`V2Compat`) |
|---|---|---|
| Session/context | RPC call → host reads `cliConfig` | Pass-through to original `conn` (still uses RPC) |
| Domain methods | RPC call → host runs V2 CLI command → V2 CAPI | Call CAPI V3 directly via go-cfclient, populate same `plugin_models` structs |
| CLI passthrough | RPC call → host runs arbitrary CLI command | Pass-through to original `conn` (still uses RPC) |
| Token refresh | RPC call → host's `repoLocator.GetAuthenticationRepository()` | Pass-through to `conn.AccessToken()` (host handles refresh) |

The generated `V2Compat` struct wraps the original `plugin.CliConnection`. Session/context methods and CLI passthrough delegate to the original RPC connection — this avoids importing `coreconfig`, reimplementing token refresh, or losing `CliCommand` functionality. The plugin still runs within `plugin.Start()` with an active RPC channel to the host.

Only the 10 domain methods are replaced. Instead of running internal V2 CLI commands via RPC, the generated code calls CAPI V3 with only the API calls needed for the fields the plugin actually uses. The `NewV2Compat` constructor builds a go-cfclient `*client.Client` from the session data (`conn.AccessToken()`, `conn.ApiEndpoint()`, `conn.IsSSLDisabled()`).

**CLI passthrough note:** `CliCommand`/`CliCommandWithoutTerminalOutput` continue to work via pass-through during the transitional migration. Plugins that use them for CAPI access (e.g., `cf curl`) should eventually migrate those calls to direct V3 API access, but this is not a blocker for adopting the generated package.

**Source files:**

| File (relative to `cloudfoundry/cli`) | Role |
|---|---|
| `plugin/plugin.go` | `CliConnection` interface definition, `Plugin` interface, `PluginMetadata`, `Command`, `Usage` types |
| `plugin/cli_connection.go` | Guest-side RPC client — dials TCP per call, gob-encoded `net/rpc` |
| `plugin/rpc/cli_rpc_server.go` | Host-side RPC server — `CliRpcCmd` with config reads and command runner |
| `plugin/rpc/run_plugin.go` | Plugin launch — `exec.Command(path, port, args...)` |
| `plugin/models/` | V2 model structs (`GetAppModel`, `GetAppsModel`, etc.) |

## Summary

| # | Decision | Choice |
|---|---|---|
| 1 | Code generation approach | `text/template` + `go/format` |
| 2 | Dependency chain handling | Hardcoded group ordering per method |
| 3 | Module location | POC in RFC repo, move to `cli-plugin-helpers` later |
| 4 | Error handling in generated code | Eager return with partial results |
| 5 | Per-item API calls | Generate the code, annotate cost in YAML |
| 6 | `include`/`fields` optimization | Use wherever CAPI V3 supports them |
| 7 | What the generated package replaces | Full `CliConnection` RPC pair — session methods copied, domain methods regenerated for V3 |

## Generate Implementation Phases

The `generate` subcommand is implemented in phases, each producing testable output.

### Phase A: Foundation ✅

- `generator/config.go` — YAML parsing into `GenerateConfig`, validated against `scanner.V2Models`
- `generator/mapping.go` — Group resolution with dependency chain forcing (Stats→Process, Stack→Droplet)
- `generator/generator.go` — Orchestrator: load config → resolve active groups → compute imports → render templates → `go/format`
- `generator/templates.go` — Template constants (migrated from `go:embed` to string constants for POC simplicity)
- Wire `runGenerate()` into `main.go`

### Phase B: Session Pass-Through + V2Compat Struct ✅

- `sessionTemplate` — `V2Compat` struct, `NewV2Compat` constructor (with/without go-cfclient), 16 session pass-through methods, 2 CLI passthrough methods
- `fileTemplate` — Package declaration, conditional imports, generated-file header with API call summary
- `helpersTemplate` — Pointer-dereference helpers (`ptrStr`, `ptrInt`, `ptrInt64`) for nullable V3 API fields

**Test milestone:** cf-targets-plugin end-to-end validation:

| Plugin | `CliConnection` Usage | What It Tests | Result |
|---|---|---|---|
| `test_rpc_server_example` | `ApiEndpoint()` + `CliCommandWithoutTerminalOutput("curl", ...)` | Session pass-through + CLI passthrough | ✅ Scan → generate pipeline works |
| `cf-targets-plugin` | None (reads `~/.cf/config.json` directly) | Zero-change migration: drop in generated file, rebuild, install, run | ✅ `cf targets` works with V2Compat |

**cf-targets-plugin migration (session-only):**
- 0 source files modified, 1 generated file added (141 lines)
- No `go get` needed — no go-cfclient dependency for session-only plugins
- `make build` → `cf install-plugin -f` → `cf targets` — works identically

### Phase C: Simple Domain Methods (single-group) ✅

- `getOrgsTemplate`, `getSpacesTemplate` — Single API call, conditional fields (Guid, Name), no sub-fields

### Phase D: Medium Domain Methods ✅

- `getServiceTemplate` — Chained lookups: ServiceInstance → ServicePlan → ServiceOffering (each conditional)
- `getServicesTemplate` — Two groups: instances list + per-instance bindings with `ListIncludeAppsAll`
- `getOrgTemplate` — Five conditional groups (Org, Quota, Spaces, Domains, SpaceQuotas)
- `getSpaceTemplate` — Six conditional groups (Space+Org, Apps, Services, Domains, SecurityGroups, SpaceQuota)
- `getOrgUsersTemplate`, `getSpaceUsersTemplate` — Role aggregation with `ListIncludeUsersAll`, user dict pattern

### Phase E: Complex Domain Methods (dependency chains, per-item) ✅

- `getAppsTemplate` — Base list + per-app loops for Process, Stats, Routes (4 conditional groups)
- `getAppTemplate` — Nine conditional groups, two dependency chains (Process→Stats, Droplet→Stack)

**Test milestone — OCF Scheduler plugin end-to-end:**
- Scan → generate pipeline: `cf-plugin-migrate scan ... | cf-plugin-migrate generate /dev/stdin -`
- Generated `getApp` uses V3 `Applications.Single` for name→GUID resolution (1 API call)
- Compiled, installed, tested against live CAPI V3 (`https://api.sys.adepttech.ca`, v3.180.0)
- `cf create-job cf-env-bionic test-v2compat-job "echo hello"` — GetApp resolved via V3, Scheduler API responded (404 = no scheduler service, but the V3 lookup succeeded)

**Migration pattern — two approaches (plugin developer chooses):**

1. **Shadow the parameter** (minimal, one line added to `Run`):
   ```go
   func (p *MyPlugin) Run(conn plugin.CliConnection, args []string) {
       conn, err := NewV2Compat(conn)
       if err != nil { fmt.Println(err); return }
       // rest unchanged — conn now uses V3 for domain methods
   ```

2. **Explicit variable** (clearer intent):
   ```go
   compat, err := NewV2Compat(cliConnection)
   services := &core.Services{CLI: compat, ...}
   ```

**OCF Scheduler migration (with domain methods):**
- 1 generated file added (`v2compat_generated.go`)
- 1 dependency added (`go get github.com/cloudfoundry/go-cfclient/v3`)
- 5 lines changed in `main.go` (wrap the connection)
- 0 changes to any other source file — `GetApp` calls in `create-job.go` and `create-call.go` transparently use V3

### Phase F: Scanner Enhancement — `CliCommand` / `cf curl` Analysis

The scanner currently detects only the 10 domain methods. Many plugins also use `CliCommand` or `CliCommandWithoutTerminalOutput` to call `cf curl` against CAPI endpoints directly. These calls are not opaque — the AST can trace the full data flow:

**What the scanner can extract:**

1. **Detect the call** — `cliConnection.CliCommandWithoutTerminalOutput("curl", "v2/apps")` or `cliConnection.CliCommand("curl", "/v3/spaces/...")`
2. **Extract the endpoint URL** — the string literal or variable passed as the curl argument (e.g., `"v2/apps"`, `"v3/service_instances"`)
3. **Trace the result** — the `[]string` return is typically `json.Unmarshal([]byte(output[0]), &targetStruct)`
4. **Walk the target struct** — resolve the struct type definition to find the JSON field tags and Go field names
5. **Track field access** — which fields of the unmarshalled struct are actually used downstream

**Example analysis** (`test_rpc_server_example`):

```go
output, err := cliConnection.CliCommandWithoutTerminalOutput("curl", nextURL)
json.Unmarshal([]byte(output[0]), &apps)
// apps.Resources[].Entity.Name, apps.Resources[].Entity.State
// apps.NextURL (pagination)
```

Scanner report:
- Endpoint: `v2/apps` → V3 equivalent: `GET /v3/apps`
- Response fields used: `Entity.Name` → `name`, `Entity.State` → `state`
- Pagination: V2 `next_url` → V3 pagination

**V2-to-V3 endpoint mapping data:**

The scanner needs a mapping of known V2 curl endpoints to their V3 equivalents, similar to how `V2Models` maps domain method fields. Common patterns:

| V2 Endpoint | V3 Equivalent | Notes |
|---|---|---|
| `v2/apps` | `/v3/apps` | Response structure differs (V2 `entity`/`metadata` → V3 flat) |
| `v2/spaces` | `/v3/spaces` | |
| `v2/organizations` | `/v3/organizations` | |
| `v2/service_instances` | `/v3/service_instances` | |
| `v2/routes` | `/v3/routes` | |
| `v2/domains` | `/v3/domains` | Private/shared distinction changed |
| `v2/users` | `/v3/users` + `/v3/roles` | Roles separated in V3 |

**Scope:** This phase produces scanner output only — it reports findings and flags the calls for manual migration. The generator does not auto-generate replacements for arbitrary `cf curl` calls, but the report tells the developer exactly what V3 endpoint and fields to use.

### Phase G: Polish

- Golden file tests for all worked examples
- CLI flags: `-config`, `-output`, `-dry-run`
- Error messages for common mistakes

## Test Environment

- **CF API:** `https://api.sys.adepttech.ca` (CAPI V3 v3.180.0)
- **Purpose:** Validate generated wrappers against a live CAPI V3 endpoint. Test with real user permissions to verify per-item API call behavior, field mapping correctness, and `include`/`fields` parameter support.

## References

- [YAML Schema](rfc-draft-plugin-transitional-migration.md#yaml-schema-cf-plugin-migrateyml) — formal `cf-plugin-migrate.yml` definition
- [Complete V2→V3 Field Mapping](rfc-draft-plugin-transitional-migration.md#complete-v2v3-field-mapping-reference) — the generator's knowledge base
- [V2 Model Struct Reference](rfc-draft-plugin-transitional-migration.md#v2-plugin-model-struct-reference) — target types for generated code
- [Automated Audit Design](rfc-draft-plugin-transitional-migration.md#automated-audit-cf-plugin-migrate-scan) — scanner approach and coverage tiers
- [Worked Example: OCF Scheduler](rfc-draft-plugin-transitional-migration.md#worked-example-ocf-scheduler-plugin) — expected generator output for a simple case
- [Worked Example: metric-registrar](rfc-draft-plugin-transitional-migration.md#worked-example-metric-registrar-plugin-complex-migration) — expected generator output for a complex case
