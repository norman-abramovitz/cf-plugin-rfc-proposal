# cf-plugin-migrate scan — Test Results and Known Issues

## Test Results

The scanner was tested against five CF CLI plugins. Results are shown with the YAML output and observations.

### 1. OCF Scheduler (`ocf-scheduler-cf-plugin`)

**Source:** `cloudfoundry/ocf-scheduler` — scheduling jobs and calls against CF apps.
- **Tested at:** `v1.2.0` (tag), commit `416d6f0` (HEAD of main, no V3 migration)

```
Found V2 domain method calls:

  commands/create-call.go:31  GetApp   → fields: Guid
  commands/create-job.go:64   GetApp   → fields: Guid
    V3 API calls: Applications.Single

  core/util.go:69             GetApps  ⚠ result returned to caller — field access may be in calling function
```

```yaml
schema_version: 1
package: main
methods:
  GetApp:
    fields: [Guid]
  GetApps:
    fields: []
```

**Observations:**
- `GetApp` correctly detected with `Guid` field — needs only `Applications.Single`.
- `GetApps` detected via `return services.CLI.GetApps()` in `MyApps()`. Flagged because the result is returned to the caller and field access (`.Guid`, `.Name`) happens in `AppByGUID()` which receives the result as a `[]models.GetAppsModel` parameter. The scanner cannot trace cross-function data flow.
- **Manual review needed:** `GetApps` fields should be `[Guid, Name]` based on code inspection of `core/util.go:72-78`.

### 2. MySQL CLI Plugin (`mysql-cli-plugin`)

**Source:** `pivotal-cf/mysql-cli-plugin` — MySQL for PCF service migration.
- **Tested at:** `v0.10.1` (tag), commit `f0866bd8` (HEAD of main, no V3 migration)

```
Found V2 domain method calls:

  mysql-tools/cf/migrator_client.go:53   GetService  → fields: LastOperation
  mysql-tools/cf/migrator_client.go:219  GetService  → fields: LastOperation
  mysql-tools/cf/migrator_client.go:308  GetService  → fields: LastOperation
    last_operation_fields: Description, State
    V3 API calls: ServiceInstances.Single(fields[service_plan], fields[service_plan.service_offering])
```

```yaml
schema_version: 1
package: app
methods:
  GetService:
    fields: [LastOperation]
    last_operation_fields: [Description, State]
```

**Observations:**
- Clean detection — all three call sites found with consistent field access.
- Sub-fields (`LastOperation.State`, `LastOperation.Description`) correctly categorized under `last_operation_fields`.
- V3 migration: single API call with `fields` parameters (Decision 6 optimization).
- This is an ideal scanner result — correct, complete, and actionable.

### 3. cf-plugin-deploy (`cf-plugin-deploy`)

**Source:** `Comcast/cf-plugin-deploy` — org/space/domain/quota provisioning.
- **Tested at:** `0.4.0` (tag), commit `1eb9db8` (HEAD of master, no V3 migration)

```
Found V2 domain method calls:

  deployer.go:258  GetApp        → fields: Routes
    route_fields: Domain.Name, Host
    V3 API calls: Routes.ListForApp(include=domain)

  deployer.go:308  GetServices   → fields:

  deployer.go:90   GetOrg        → fields: Domains, Guid, SpaceQuotas, Spaces
  deployer.go:103  GetOrg        → fields: Domains, Guid, SpaceQuotas, Spaces
  deployer.go:124  GetOrg        → fields: Domains, Guid, SpaceQuotas, Spaces
  deployer.go:148  GetOrg        → fields: Domains, Guid, SpaceQuotas, Spaces
  deployer.go:177  GetOrg        → fields: Domains, Guid, SpaceQuotas, Spaces
  deployer.go:380  GetOrg        → fields: Domains, Guid, SpaceQuotas, Spaces
    domain_fields: Name
    space_fields: Name
    space_quota_fields: Name
    V3 API calls: Organizations.Single, Spaces.ListAll, Domains.ListForOrganization, SpaceQuotas.ListAll

  deployer.go:185  GetSpace      → fields:
  deployer.go:129  GetOrgUsers   → fields:
  deployer.go:190  GetSpaceUsers → fields:
```

```yaml
schema_version: 1
package: main
methods:
  GetApp:
    fields: [Routes]
    route_fields: [Domain.Name, Host]
  GetServices:
    fields: []
  GetOrg:
    fields: [Domains, Guid, SpaceQuotas, Spaces]
    domain_fields: [Name]
    space_fields: [Name]
    space_quota_fields: [Name]
  GetSpace:
    fields: []
  GetOrgUsers:
    fields: []
  GetSpaceUsers:
    fields: []
```

**Observations:**
- Richest scan result — 6 of 10 V2 methods detected across one file.
- `GetOrg` correctly identified with 4 field groups and 3 sub-field categories.
- `GetApp` routes with sub-fields (`Domain.Name`, `Host`) correctly categorized.
- `GetServices`, `GetSpace`, `GetOrgUsers`, `GetSpaceUsers` have empty fields — the plugin uses these calls to validate that input parameters (org name, space name, etc.) refer to real CF resources. The error return confirms existence. Generated wrappers must still make the V3 API call and return the error.

### 4. App Autoscaler CLI Plugin (`app-autoscaler-cli-plugin`, pre-V3 commit)

**Source:** `cloudfoundry/app-autoscaler-cli-plugin`
- **Last pre-V3 release:** `v3.0.0` (tag)
- **Last pre-V3 commit:** `bdbc163` (before `38bbeb9` "Switch to V3 CF API client")
- **Test command:** `git checkout v3.0.0`

```
Found V2 domain method calls:

  src/cli/api/cfclient.go:67  GetApp  → fields: Guid
    V3 API calls: Applications.Single
```

```yaml
schema_version: 1
package: api
methods:
  GetApp:
    fields: [Guid]
```

**Observations:**
- Simplest possible case — one method, one field.
- This plugin has already migrated to V3 independently (commit `38bbeb9`). Testing the pre-migration version validates the scanner against a known migration case.
- The actual V3 migration they did confirms our scan — they only needed the app GUID.

### 5. multiapps-cli-plugin (`multiapps-cli-plugin`, pre-V3 commit)

**Source:** `cloudfoundry/multiapps-cli-plugin`
- **Last pre-V3 release:** `v2.8.0` (tag, commit `393f52b` "Set version to 2.8.0")
- **Last pre-V3 commit:** `2276616` (before `31918f1` "Update calls to CF to use only V3 API")
- **Test command:** `git checkout v2.8.0`

```
Found V2 domain method calls:

  commands/mta_command.go:89   GetApps     → fields:
  commands/mta_command.go:107  GetServices → fields:
  commands/base_command.go:174 GetOrg      → fields:
  commands/base_command.go:178 GetSpace    → fields:
```

```yaml
schema_version: 1
package: fakes
methods:
  GetApps:
    fields: []
  GetServices:
    fields: []
  GetOrg:
    fields: []
  GetSpace:
    fields: []
```

**Observations:**
- **`GetOrg` and `GetSpace` are false positives** (Issue #1). These are local wrapper methods on `CloudFoundryContext` that call `GetCurrentOrg()` and `GetCurrentSpace()` — session-context methods, not V2 domain methods. The scanner matched by name alone.
- **`GetApps` and `GetServices` are correct calls but with empty fields** (Issue #2). The results are iterated in `mta_command.go` and passed to helper functions (`getInstances()`, `getRoutes()`, `getLastOperation()`, `isMtaAssociatedApp()`, `isMtaAssociatedService()`) where the actual field access happens. Cross-function data flow is not traced.
- **Manual review reveals heavy field usage:**
  - `GetApps`: `Name`, `State`, `RunningInstances`, `TotalInstances`, `Memory`, `DiskQuota`, `Routes[].Host`, `Routes[].Domain.Name`
  - `GetServices`: `Name`, `Service.Name`, `ServicePlan.Name`, `LastOperation.Type`, `LastOperation.State`, `ApplicationNames`
- This is the most complex consumer tested — the correct YAML after manual review would be:

```yaml
methods:
  GetApps:
    fields: [DiskQuota, Memory, Name, RunningInstances, State, TotalInstances, Routes]
    route_fields: [Domain.Name, Host]
  GetServices:
    fields: [ApplicationNames, LastOperation, Name, Service, ServicePlan]
    last_operation_fields: [State, Type]
    service_fields: [Name]
    service_plan_fields: [Name]
```

- The plugin's own comment (line 85-88) notes they chose `GetApps()` over `GetApp()` because it's faster and avoids failure on unstaged apps — this validates our per-item call analysis.
- This plugin has already migrated to V3 independently (commit `31918f1`).

### 6. cf-mgmt (`cf-mgmt`)

**Source:** `vmware-tanzu-labs/cf-mgmt` — Cloud Foundry configuration management.
- **Tested at:** commit `428fe78` (HEAD of main, no tags, no V3 migration)

```
Found V2 domain method calls:

  configcommands/global.go:147       GetService  → fields:
  serviceaccess/service_info.go:106  GetService  → fields:
```

**Observations:**
- **False positive.** Both calls are `broker.GetService(serviceName)` — a method on a local `ServiceBroker` type, not the CF CLI plugin `CliConnection.GetService()`. The scanner matches by method name only and cannot distinguish between V2 plugin methods and local methods with the same name.
- This is a known limitation of name-based matching without type information (see Known Issues #1).

## Known Issues

### Issue 1: No Type Information — False Positives on Method Names

**Severity:** Low (easily identified by reviewing empty-field results)

The scanner uses `go/ast` and `go/parser` for source analysis. It matches V2 method calls by name (e.g., any `.GetService()` call), without type information from `go/types`. This means local methods with the same name as V2 methods produce false positives.

**Mitigation:** False positives typically have empty field lists because the local type's fields don't match the V2 model's `FieldGroup` map. Reviewing the YAML output for empty-field methods will catch most cases.

**Fix:** Use `go/types` with `golang.org/x/tools/go/packages` for type-checked analysis. This would require the target module to be buildable (all dependencies present). Deferred — the current approach is adequate for a POC where results are reviewed by the developer.

### Issue 2: Cross-Function Data Flow Not Traced

**Severity:** Medium (correctly flagged but fields not populated)

When a V2 method result is returned from the calling function, passed as a parameter, or stored in a struct field, the scanner cannot trace which fields are accessed in other functions. These calls are detected and flagged with a warning:

```
core/util.go:69  GetApps  ⚠ result returned to caller — field access may be in calling function
```

**Current patterns detected:**
- `return conn.GetApps()` — ReturnStmt (flagged)
- `app, err := conn.GetApp(name)` followed by `app.Guid` — AssignStmt + direct field access (fully traced)
- `for _, route := range app.Routes` followed by `route.Host` — range variable tracking (fully traced)

**Patterns NOT detected:**
- Result passed as function argument: `processApp(conn.GetApp(name))`
- Result stored in struct field: `s.app = conn.GetApp(name)` (then `s.app.Guid` elsewhere)
- Result stored in map or slice: `apps[name] = conn.GetApp(name)`
- Multi-return destructuring beyond first variable: `app, warnings, err := ...`

**Mitigation:** Flagged call sites alert the developer to perform manual review. The YAML file can be hand-edited to add the missing fields before running `generate`.

### Issue 3: Variable Shadowing and Scope Leaks

**Severity:** Low (fixed for the common case)

The scanner walks the entire function body in Phase 2 without tracking variable scope boundaries. If a range variable shadows another variable name, field access on the shadowing variable could be misattributed.

**Example from cf-plugin-deploy:**
```go
o, _ := d.cf.GetOrg(org)          // d = method receiver
for _, d := range o.Domains {      // d = range variable, shadows receiver
    if d.Name == domain { ... }    // d.Name = Domain.Name ✓
}
d.run("share-private-domain"...)   // d = back to receiver, but scanner
                                   // sees range var accessing .run
```

**Fix applied:** Sub-field names must start with an uppercase letter (exported). V2 model struct fields are always exported. This filters out scope-leak false positives like `run`, `cf.GetOrg` which are unexported method/field access on the shadowed receiver.

**Residual risk:** If a shadowed variable accesses an exported field that happens to match a V2 sub-field name, it would be a false positive. This is unlikely in practice.

### Issue 4: Empty-Field Methods Are Existence Checks

**Severity:** Informational

Several methods in cf-plugin-deploy are detected with empty field lists:
```yaml
GetServices:
  fields: []
GetSpace:
  fields: []
GetOrgUsers:
  fields: []
GetSpaceUsers:
  fields: []
```

These calls validate that input parameters (org name, space name, etc.) refer to real CF resources. The plugin checks the error return to confirm the resource exists before proceeding. The V3 API call is required even though no fields are populated on the model.

**Recommendation:** The `generate` command should produce a minimal wrapper for empty-field methods that makes the base API call (e.g., `Organizations.Single`, `Spaces.Single`) and returns the error. No field population code is needed, but the call itself is required for input validation.

## Scanner Capabilities Summary

| Pattern | Detected | Fields Traced |
|---|---|---|
| `app, err := conn.GetApp(name)` then `app.Guid` | Yes | Yes |
| `for _, r := range app.Routes` then `r.Host` | Yes | Yes |
| `app.Routes[i].Domain.Name` (indexed access) | Yes | Yes |
| `return conn.GetApps()` | Yes | No (flagged) |
| `services.CLI.GetApps()` (deep selector chain) | Yes | Depends on context |
| `processApp(conn.GetApp(name))` | No | No |
| `s.app, err = conn.GetApp(name)` (struct field) | No | No |
| `broker.GetService(name)` (same-name local method) | Yes (false positive) | No (empty fields) |

## Test Environment

- **Scanner version:** `cf-plugin-migrate` built from `cf-plugin-migrate-tool` branch
- **Go version:** 1.22+
- **Date:** 2026-03-08
- **Plugins tested:** OCF Scheduler, MySQL CLI Plugin, cf-plugin-deploy, App Autoscaler CLI Plugin (pre-V3), multiapps-cli-plugin (pre-V3), cf-mgmt
