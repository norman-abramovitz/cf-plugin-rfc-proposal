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

## Summary

| # | Decision | Choice |
|---|---|---|
| 1 | Code generation approach | `text/template` + `go/format` |
| 2 | Dependency chain handling | Hardcoded group ordering per method |
| 3 | Module location | POC in RFC repo, move to `cli-plugin-helpers` later |
| 4 | Error handling in generated code | Eager return with partial results |
| 5 | Per-item API calls | Generate the code, annotate cost in YAML |
| 6 | `include`/`fields` optimization | Use wherever CAPI V3 supports them |

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
