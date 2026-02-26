# CF CLI Plugin Interface Survey

This document captures the results of surveying actively maintained CF CLI plugins
to understand how they use the current plugin interface. The findings inform the
[RFC Draft: CLI Plugin Interface V2](rfc-draft-cli-plugin-interface-v2.md).

**Methodology:** For each plugin, we examined the source code to identify every
call to `plugin.CliConnection` methods and how the plugin interacts with Cloud
Foundry (plugin API, direct CAPI calls, `CliCommand` delegation, or external
process execution).

---

## Summary Matrix

### Core Context Methods

| Method | OCF Sched | Autoscaler | MTA | cf-java | cf-targets | Rabobank | upgrade-all | stack-auditor | log-cache | defaultenv | metric-reg | svc-inst-logs | spring-cloud | mysql-cli | swisscom | html5 | cf-lookup | list-svc |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| `AccessToken()` | Y | Y | Y | - | - | Y | Y | - | Y | Y | - | Y | Y | Y | - | Y | - | - |
| `ApiEndpoint()` | Y | Y | Y | - | - | Y | Y | - | Y | Y | - | - | Y | Y | - | Y | - | - |
| `IsSSLDisabled()` | - | Y | Y | - | - | Y | Y | - | Y | - | - | - | Y | Y | - | Y | - | - |
| `IsLoggedIn()` | Y | Y | - | - | - | Y | Y | - | - | - | - | - | - | - | - | - | Y | Y |
| `GetCurrentOrg()` | Y | - | Y | - | - | Y | - | - | Y | - | - | Y | Y | - | - | Y | - | - |
| `GetCurrentSpace()` | Y | Y | Y | - | - | Y | - | Y | Y | Y | Y | Y | Y | Y | Y | Y | - | - |
| `HasOrganization()` | - | - | - | - | - | Y | - | - | - | - | - | - | - | - | - | - | - | Y |
| `HasSpace()` | - | Y | - | - | - | Y | - | - | - | - | - | - | - | - | - | - | - | Y |
| `HasAPIEndpoint()` | Y | - | - | - | - | - | - | - | Y | - | - | - | - | - | - | - | Y | - |
| `Username()` | Y | - | Y | - | - | Y | - | - | Y | - | - | Y | Y | - | Y | Y | - | - |
| `ApiVersion()` | - | - | - | - | - | - | Y | - | - | - | - | - | - | - | - | - | - | - |

### Domain Model Methods (V2-coupled)

| Method | OCF Sched | Autoscaler | MTA | cf-java | cf-targets | Rabobank | upgrade-all | stack-auditor | log-cache | defaultenv | metric-reg | svc-inst-logs | spring-cloud | mysql-cli | swisscom | html5 | cf-lookup | list-svc |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| `GetApp()` | Y | **Removed** | - | - | - | - | - | - | - | - | Y | - | - | - | - | - | - | Y |
| `GetApps()` | Y | - | - | - | - | - | - | - | - | - | Y | - | Y | - | - | - | - | - |
| `GetService()` | - | - | - | - | - | - | - | - | - | - | - | Y | Y | Y | Y | - | - | - |
| `GetServices()` | - | - | - | - | - | - | - | - | - | - | Y | - | - | - | - | - | - | - |
| `GetOrg()` | - | - | - | - | - | - | - | - | - | - | - | - | - | - | Y | - | - | - |
| `GetOrgs()` | - | - | - | - | - | - | - | Y | - | - | - | - | - | - | - | - | - | - |

### CLI Command Delegation

| Method | OCF Sched | Autoscaler | MTA | cf-java | cf-targets | Rabobank | upgrade-all | stack-auditor | log-cache | defaultenv | metric-reg | svc-inst-logs | spring-cloud | mysql-cli | swisscom | html5 | cf-lookup | list-svc |
|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| `CliCommand()` | - | - | help | **Removed** | - | - | - | - | - | - | - | - | - | Y | - | - | Y | Y |
| `CliCmdWithoutTermOut()` | - | - | version | **Removed** | - | - | - | Y | Y | - | Y | Y | - | Y | Y | Y | - | Y |

### CF Interaction Pattern

| Plugin | Pattern |
|---|---|
| OCF Scheduler | Plugin API context + direct HTTP to scheduler API |
| App Autoscaler | Plugin API context + go-cfclient V3 for app lookup + direct HTTP to autoscaler API |
| MultiApps (MTA) | Plugin API context + direct CAPI V3 HTTP + deploy-service API |
| cf-java-plugin | `exec.Command("cf", ...)` for everything; plugin API completely unused at runtime |
| cf-targets-plugin | Direct `~/.cf/config.json` file I/O; plugin API completely unused |
| Rabobank plugins | Plugin API context + go-cfclient V3 + direct HTTP to service APIs |
| upgrade-all-services | Plugin API context + direct CAPI V3 HTTP (custom requester) |
| stack-auditor | `CliCommandWithoutTerminalOutput("curl", ...)` + `GetOrgs()` + `exec.Command("cf", "restage", ...)` |
| log-cache-cli | Plugin API context + `CliCommandWithoutTerminalOutput` for GUID resolution + go-log-cache library |
| DefaultEnv | Plugin API context + go-cfclient V3 |
| metric-registrar | `CliCommandWithoutTerminalOutput` for CLI commands + V2 curl + `GetApp`/`GetApps`/`GetServices` |
| service-instance-logs | `CliCommandWithoutTerminalOutput` V2 curl + `GetService()` + direct HTTP to log endpoint |
| spring-cloud-services | Plugin API context + `GetService()` + `GetApps()` + direct HTTP to SCS broker |
| mysql-cli-plugin | `CliCommandWithoutTerminalOutput` for CLI commands + V3 curl + go-cfclient V2 + `GetService()` |
| swisscom appcloud | `CliCommandWithoutTerminalOutput("curl", ...)` for all API access + `GetService()`/`GetOrg()` |
| html5-apps-repo | Hybrid: `CliCommandWithoutTerminalOutput("curl", ...)` for reads, direct HTTP for writes + UAA token exchange |
| cf-lookup-route | go-cfclient V3 (reads `~/.cf/config.json` directly); plugin API only for validation |
| list-services | `GetApp()` + `CliCommandWithoutTerminalOutput("curl", "/v3/service_bindings...")` |

---

## Detailed Plugin Analyses

### 1. OCF Scheduler (`cloudfoundry-community/ocf-scheduler-cf-plugin`)

- **Last updated:** Active (2026)
- **Plugin API methods used (7):** `IsLoggedIn`, `AccessToken`, `HasAPIEndpoint`, `ApiEndpoint`, `GetCurrentOrg`, `GetCurrentSpace`, `UserEmail`, `GetApp`, `GetApps`
- **CF interaction:** Plugin API for auth/context and app lookups; all scheduler operations via direct HTTP to scheduler service API
- **URL discovery:** Derives scheduler URL from CF API endpoint by hostname substitution (`api.` → `scheduler.`)
- **Dependencies:** `code.cloudfoundry.org/cli`, `github.com/ess/hype` (HTTP client), `github.com/cloudfoundry-community/ocf-scheduler` (models)
- **Notes:** Still uses `GetApp()`/`GetApps()` (V2-coupled methods). Uses `cf/terminal` for UI components.

### 2. App Autoscaler (`cloudfoundry/app-autoscaler-cli-plugin`)

- **Last updated:** Active (2026)
- **Plugin API methods used (6):** `ApiEndpoint`, `HasSpace`, `IsLoggedIn`, `AccessToken`, `GetCurrentSpace`, `IsSSLDisabled`
- **CF interaction:** Defines a custom narrow `Connection` interface with only 6 methods. Uses go-cfclient V3 for app GUID lookup via CAPI V3. All autoscaler operations via direct HTTP.
- **V2→V3 migration:** PR #132 removed `GetApp()` (V2) and replaced with `go-cfclient/v3` `Applications.ListAll()`. Added `HasSpace()` and `GetCurrentSpace()` to compensate.
- **Dependencies:** `code.cloudfoundry.org/cli/v8`, `github.com/cloudfoundry/go-cfclient/v3` (alpha.19), `github.com/jessevdk/go-flags`
- **Pain points:** `IsSSLDisabled()` was not correctly forwarded initially (required follow-up fix). Token refresh not supported during long operations since go-cfclient gets an empty refresh token.

### 3. MultiApps / MTA (`cloudfoundry/multiapps-cli-plugin`)

- **Last updated:** Active (2026)
- **Plugin API methods used (8):** `AccessToken`, `GetCurrentOrg`, `GetCurrentSpace`, `Username`, `ApiEndpoint`, `IsSSLDisabled`, `CliCommandWithoutTerminalOutput` (version detection), `CliCommand` (help display)
- **CF interaction:** Two-layer architecture. Plugin API for session/context only. Direct CAPI V3 HTTP for all domain operations (apps, services, routes, service bindings via label selectors). Bulk of work goes to custom MTA deploy-service API.
- **Token handling:** Implements JWT-based token caching — parses `exp` claim, refreshes at halfway point.
- **Dependencies:** `code.cloudfoundry.org/cli/v8`, `code.cloudfoundry.org/jsonry`, `github.com/go-openapi/*` (Swagger), `github.com/golang-jwt/jwt/v5`
- **Pain points:** Creates new `http.Transport` per request for CAPI V3 calls (inefficient). URL discovery via string manipulation of API endpoint.

### 4. cf-java-plugin (`SAP/cf-cli-java-plugin`)

- **Last updated:** Active (2026)
- **Plugin API methods used: NONE** (as of v4.0.2)
- **CF interaction:** All CF interaction via `exec.Command("cf", ...)`. Uses `cf curl /v3/apps/{GUID}/env` and `cf curl /v3/apps/{GUID}/ssh_enabled` for CAPI V3. The `cliConnection` parameter is marked `_` (ignored).
- **History:** Originally used `CliConnection.CliCommand()` for `cf ssh`. Abandoned it entirely due to authentication failures where `cf ssh` via the plugin API failed but worked directly from terminal.
- **Dependencies:** `code.cloudfoundry.org/cli` (types only), `github.com/simonleung8/flags` (last updated 2017), `github.com/lithammer/fuzzysearch`
- **Pain points:** `CliCommand()` unreliable for `cf ssh`. No stdout/stderr separation. Flag parsing library from 2017 is unmaintained.

### 5. cf-targets-plugin (`cloudfoundry-community/cf-targets-plugin`)

- **Last updated:** Active (2026, develop branch)
- **Plugin API methods used: NONE**
- **CF interaction:** Directly reads/writes `~/.cf/config.json` using internal CF CLI packages (`cf/configuration`, `cf/configuration/confighelpers`, `cf/configuration/coreconfig`). Saves/restores target configs as files in `~/.cf/targets/`.
- **Dependencies:** `code.cloudfoundry.org/cli` (for internal config packages, not plugin API)
- **Pain points:** Massive dependency tree (Google Cloud SDK, AWS SDK, BOSH CLI, k8s client-go) pulled in transitively just for config file helpers. Plugin API gap: no way to save/restore CLI configuration, so it bypasses the interface.

### 6. Rabobank cf-plugins (`rabobank/cf-plugins` + 4 consumers)

- **Last updated:** Active (2025-2026)
- **Library methods used (16 pass-through):** All standard context methods plus `UserGuid`, `UserEmail`, `LoggregatorEndpoint`, `DopplerEndpoint`
- **Library methods reimplemented via V3:** `GetApp`, `GetApps`, `GetOrgs`, `GetSpaces`, `GetOrgUsers`, `GetSpaceUsers`, `GetServices`, `GetService`, `GetOrg`, `GetSpace`
- **Consumer plugin usage:** All 4 consumer plugins primarily use only `AccessToken`, `ApiEndpoint`, `GetCurrentOrg`, `GetCurrentSpace`, `Username`, `IsLoggedIn`, `HasOrganization`, `HasSpace`. Only credhub-plugin uses a reimplemented method (`GetService`).
- **CF interaction:** Library wraps `plugin.CliConnection` with V3 reimplementations. Provides `CfClient()` for direct go-cfclient V3 access. Consumer plugins do their own direct HTTP to service APIs.
- **Dependencies:** `code.cloudfoundry.org/cli v7.1.0+incompatible`, `github.com/cloudfoundry/go-cfclient/v3` (alpha.15)
- **Pain points:** `GetApp()` reimplementation requires 11 V3 API calls. Token prefix stripping hardcodes 7-char assumption. Hardcoded user agent version.

### 7. upgrade-all-services (`cloudfoundry/upgrade-all-services-cli-plugin`)

- **Last updated:** Active (2026)
- **Plugin API methods used (5):** `IsLoggedIn`, `ApiVersion`, `AccessToken`, `ApiEndpoint`, `IsSSLDisabled`
- **CF interaction:** "Bootstrap-then-bypass" pattern. Uses plugin API only at startup to extract credentials/endpoint, then makes all CAPI V3 calls via custom `Requester` (direct HTTP with `jsonry` for JSON).
- **CAPI V3 endpoints:** `GET /v3/service_plans`, `GET /v3/service_instances`, `PATCH /v3/service_instances/{guid}` (upgrade trigger), `GET /v3/service_instances/{guid}` (poll)
- **Dependencies:** `code.cloudfoundry.org/cli/v8`, `code.cloudfoundry.org/jsonry`, `github.com/blang/semver/v4`, `github.com/hashicorp/go-version`
- **Notes:** Does NOT use go-cfclient — rolls its own minimal HTTP requester. Defines its own narrow `CLIConnection` interface with only 5 methods.

### 8. stack-auditor (`cloudfoundry/stack-auditor`)

- **Last updated:** Active (2026)
- **Plugin API methods used (3):** `CliCommandWithoutTerminalOutput`, `GetOrgs`, `GetCurrentSpace`
- **CF interaction:** Primarily `CliCommandWithoutTerminalOutput("curl", ...)` for both V2 and V3 CAPI endpoints. Uses `exec.Command("cf", "restage", ...)` for long-running restage operations (bypasses plugin API).
- **CAPI endpoints:** `/v3/apps` (list/patch), `/v3/apps/{guid}/actions/start|stop`, `/v2/spaces`, `/v2/buildpacks`, `/v2/stacks/{guid}` (delete)
- **Dependencies:** `code.cloudfoundry.org/cli v7.1.0+incompatible`, `github.com/golang/mock`
- **Pain points:** Mixed V2/V3 API usage. Stack deletion and buildpack listing still use V2 endpoints. Uses `exec.Command` for restage because `CliCommand` is problematic for long-running operations.

### 9. log-cache-cli (`cloudfoundry/log-cache-cli`)

- **Last updated:** Active (2026)
- **Plugin API methods used (8):** `IsSSLDisabled`, `HasAPIEndpoint`, `ApiEndpoint`, `AccessToken`, `Username`, `GetCurrentOrg`, `GetCurrentSpace`, `CliCommandWithoutTerminalOutput`
- **CF interaction:** Uses `AccessToken()` as a lazy token provider for the go-log-cache HTTP client. Uses `CliCommandWithoutTerminalOutput` for GUID resolution (`cf app --guid`, `cf service --guid`) and bulk CAPI lookups (`curl /v3/apps?guids=...`, `curl /v3/service_instances?guids=...`). All log/metric data fetched directly from Log Cache HTTP API.
- **URL discovery:** `strings.Replace(apiEndpoint, "api", "log-cache", 1)` — fragile hostname substitution.
- **Dependencies:** `code.cloudfoundry.org/cli v7.1.0+incompatible`, `code.cloudfoundry.org/go-log-cache/v3`, `code.cloudfoundry.org/go-loggregator/v10`, `github.com/jessevdk/go-flags`

### 10. DefaultEnv (`SAP/cf-cli-defaultenv-plugin`)

- **Last updated:** Active (2026)
- **Plugin API methods used (3):** `AccessToken`, `ApiEndpoint`, `GetCurrentSpace`
- **CF interaction:** Minimal bootstrap — extracts token and endpoint to create a go-cfclient V3 client. Uses `client.Applications.Single()` for app lookup and `client.ExecuteAuthRequest()` for raw CAPI V3 call to `/v3/apps/{guid}/env`.
- **Dependencies:** `code.cloudfoundry.org/cli v7.1.0+incompatible`, `github.com/cloudfoundry/go-cfclient/v3` (alpha.17)
- **Notes:** Single-file plugin. Clean example of the minimal "bootstrap-then-bypass" pattern.

### 11. metric-registrar (`pivotal-cf/metric-registrar-cli`)

- **Last updated:** Active (2026)
- **Plugin API methods used (5):** `CliCommandWithoutTerminalOutput`, `GetServices`, `GetApp`, `GetApps`, `GetCurrentSpace`
- **CF interaction:** Heavy use of `CliCommandWithoutTerminalOutput` for both CLI commands (`create-user-provided-service`, `bind-service`, `unbind-service`, `delete-service`) and CAPI V2 `curl` calls (`/v2/user_provided_service_instances`, `/v2/apps/{guid}`). Uses V2-coupled model methods (`GetApp`, `GetApps`, `GetServices`).
- **Dependencies:** `code.cloudfoundry.org/cli v7.1.0+incompatible`, `github.com/jessevdk/go-flags`
- **Pain points:** Still heavily dependent on V2 API endpoints and V2-shaped plugin model methods. Would require significant migration effort.

### 12. service-instance-logs (`pivotal-cf/service-instance-logs-cli-plugin`)

- **Last updated:** Active (2026)
- **Plugin API methods used (6):** `AccessToken`, `GetService`, `CliCommandWithoutTerminalOutput`, `GetCurrentOrg`, `GetCurrentSpace`, `Username`
- **CF interaction:** Uses `GetService()` to bootstrap, then `CliCommandWithoutTerminalOutput("curl", ...)` to walk V2 API chain (service plan → service → metadata) to discover log endpoint. Then makes direct WebSocket HTTP calls to stream logs.
- **Dependencies:** `code.cloudfoundry.org/cli v7.1.0+incompatible`, `github.com/cloudfoundry/noaa` (log streaming), `github.com/cloudfoundry/sonde-go`
- **Pain points:** Entirely dependent on V2 CAPI endpoints for service metadata traversal.

### 13. spring-cloud-services (`pivotal-cf/spring-cloud-services-cli-plugin`)

- **Last updated:** Active (2026)
- **Plugin API methods used (8):** `AccessToken`, `IsSSLDisabled`, `GetService`, `ApiEndpoint`, `GetCurrentOrg`, `GetCurrentSpace`, `Username`, `GetApps`
- **CF interaction:** Uses plugin API for context and service resolution. All actual SCS operations via direct HTTP to SCS broker/instance endpoints (e.g., `/cli/instance/{guid}`, `/eureka/apps`, `/actuator/info`). Custom `AuthenticatedClient` wraps HTTP calls with bearer token. Does NOT use `CliCommand` or `CliCommandWithoutTerminalOutput`.
- **Dependencies:** `code.cloudfoundry.org/cli v7.1.0+incompatible`, `code.cloudfoundry.org/bytefmt`, `github.com/fatih/color`
- **Notes:** Clean "plugin API for context, direct HTTP for operations" pattern. Uses `GetService()` and `GetApps()` (V2-coupled).

### 14. mysql-cli-plugin (`pivotal-cf/mysql-cli-plugin`)

- **Last updated:** Active (2026)
- **Plugin API methods used (7):** `CliCommandWithoutTerminalOutput` (heavy), `CliCommand`, `GetCurrentSpace`, `GetService`, `AccessToken`, `ApiEndpoint`, `IsSSLDisabled`
- **CF interaction:** Most complex interaction pattern. The migrate workflow uses `CliCommandWithoutTerminalOutput` for CLI commands (push, bind, start, delete, rename-service, create-service-key) and `curl` for V3 CAPI (`/v3/apps`, `/v3/tasks`). The find-bindings workflow uses go-cfclient V2 library for direct CAPI access. Token refresh on `CF-InvalidAuthToken` errors.
- **Dependencies:** `code.cloudfoundry.org/cli v7.1.0+incompatible`, `github.com/cloudfoundry-community/go-cfclient/v2`, `github.com/go-sql-driver/mysql`, `github.com/jessevdk/go-flags`
- **Pain points:** Uses both V2 (go-cfclient) and V3 (curl) CAPI endpoints. Retry logic with exponential backoff. Long-running task polling requires token refresh.

### 15. Swisscom appcloud (`swisscom/appcloud-cf-cli-plugin`)

- **Last updated:** Active (2026)
- **Plugin API methods used (5):** `Username`, `GetService`, `GetCurrentSpace`, `GetOrg`, `CliCommandWithoutTerminalOutput`
- **CF interaction:** Exclusively `CliCommandWithoutTerminalOutput("curl", ...)` for all API access. Endpoints are mostly custom Swisscom `/custom/*` extensions plus one standard `/v3/audit_events`. Uses V2-coupled model methods (`GetService`, `GetOrg`).
- **Dependencies:** `code.cloudfoundry.org/cli v7.1.0+incompatible`, `github.com/pkg/errors`
- **Notes:** Uses CF CLI internal packages (`cf/terminal`, `cf/flags`, `cf/trace`) for rich terminal output.

### 16. html5-apps-repo (`SAP/cf-html5-apps-repo-cli-plugin`)

- **Last updated:** 2025
- **Plugin API methods used (7):** `Username`, `GetCurrentOrg`, `GetCurrentSpace`, `IsSSLDisabled`, `ApiEndpoint`, `AccessToken`, `CliCommandWithoutTerminalOutput`
- **CF interaction:** Hybrid pattern. Read operations use `CliCommandWithoutTerminalOutput("curl", "/v3/...")`. Write operations (create/delete service keys/instances) use direct HTTP with `AccessToken()` and `ApiEndpoint()` because they need to read `Location` headers from async 202 responses. Also performs UAA `client_credentials` token exchange for html5-apps-repo service access.
- **Dependencies:** Vendored (no go.mod). Uses older `github.com/cloudfoundry/cli` import path.
- **Notes:** Most sophisticated pattern — needs direct HTTP for writes because `CliCommandWithoutTerminalOutput("curl", ...)` doesn't expose response headers.

### 17. cf-lookup-route (`cloudfoundry/cf-lookup-route`)

- **Last updated:** 2024
- **Plugin API methods used (3):** `HasAPIEndpoint`, `IsLoggedIn`, `CliCommand` (optional re-targeting)
- **CF interaction:** Uses go-cfclient V3 library initialized via `config.NewFromCFHome()` (reads `~/.cf/config.json` directly). All domain operations via typed go-cfclient calls: `Domains.ListAll()`, `Routes.ListAll()`, `Applications.ListAll()`, `Spaces.GetIncludeOrganization()`.
- **Dependencies:** `code.cloudfoundry.org/cli v7.1.0+incompatible`, `github.com/cloudfoundry/go-cfclient/v3` (alpha.9)
- **Notes:** Bypasses plugin `CliConnection` for data access entirely. Reads `~/.cf/config.json` directly for go-cfclient initialization — similar to cf-targets-plugin's approach.

### 18. list-services (`pavellom/list-services-plugin`)

- **Last updated:** 2025
- **Plugin API methods used (6):** `IsLoggedIn`, `HasOrganization`, `HasSpace`, `GetApp`, `CliCommand` (help), `CliCommandWithoutTerminalOutput` (curl)
- **CF interaction:** Uses `GetApp()` for app GUID resolution (V2-coupled), then `CliCommandWithoutTerminalOutput("curl", "/v3/service_bindings?app_guids=...")` for service binding lookup with manual pagination.
- **Dependencies:** No `go.mod` (pre-modules). `code.cloudfoundry.org/cli/plugin` only.
- **Notes:** Simple plugin. Still uses V2-coupled `GetApp()`. Pre-modules Go project.

---

## Key Findings

### 1. Universal Core Methods

The following methods are used by nearly every plugin that uses the plugin API:

| Method | Usage Rate | Notes |
|---|---|---|
| `AccessToken()` | 12/18 | Universal for plugins that make direct API calls |
| `ApiEndpoint()` | 11/18 | Universal for URL construction and client initialization |
| `GetCurrentSpace()` | 14/18 | The most widely used context method |
| `GetCurrentOrg()` | 8/18 | Common but not universal (some plugins don't need org context) |
| `Username()` | 9/18 | Primarily for display purposes ("as USERNAME...") |
| `IsSSLDisabled()` | 8/18 | Required for TLS configuration when making direct HTTP calls |

### 2. Domain Methods Are Being Abandoned

| Method | Still Used By | Status |
|---|---|---|
| `GetApp()` | OCF Scheduler, metric-registrar, list-services | Active, but Autoscaler **removed** it |
| `GetApps()` | OCF Scheduler, metric-registrar, spring-cloud-services | Active |
| `GetService()` | service-instance-logs, spring-cloud-services, mysql-cli, swisscom | Active but returns V2 models |
| `GetServices()` | metric-registrar | Nearly abandoned |
| `GetOrg()` | swisscom | Nearly abandoned |
| `GetOrgs()` | stack-auditor | Nearly abandoned |

### 3. `CliCommand` / `CliCommandWithoutTerminalOutput` Patterns

Three distinct usage patterns exist:

1. **CLI command delegation** (e.g., `"bind-service"`, `"restage"`, `"push"`): Used by mysql-cli, metric-registrar, stack-auditor, cf-lookup-route. These plugins orchestrate CF CLI commands as workflow steps.

2. **`cf curl` for CAPI access** (e.g., `"curl"`, `"/v3/apps?..."`): Used by stack-auditor, log-cache, metric-registrar, html5, swisscom, list-services, service-instance-logs, mysql-cli. This is the most common pattern for accessing V3 CAPI endpoints without building a custom HTTP client.

3. **GUID resolution** (e.g., `"app"`, appName, `"--guid"`): Used by log-cache. A targeted pattern for resolving names to GUIDs.

### 4. Direct CAPI V3 is the Direction

Plugins that have been recently updated or migrated consistently move toward direct CAPI V3 access:

- **go-cfclient V3:** Autoscaler, DefaultEnv, cf-lookup-route, Rabobank
- **Custom HTTP client:** upgrade-all-services, MTA, html5 (writes)
- **`cf curl` via CliCommandWithoutTerminalOutput:** stack-auditor, log-cache, html5 (reads), swisscom, list-services

### 5. Plugins That Bypass the Plugin API Entirely

- **cf-java-plugin:** `cliConnection` parameter is `_` (unused). All CF interaction via `exec.Command("cf", ...)`.
- **cf-targets-plugin:** Reads `~/.cf/config.json` directly. Plugin API completely unused.
- **cf-lookup-route:** Uses plugin API for validation only. Reads `~/.cf/config.json` for go-cfclient initialization.

### 6. URL Discovery is Fragile

Multiple plugins derive service URLs from the CF API endpoint by hostname substitution:
- OCF Scheduler: `api.` → `scheduler.`
- Log Cache: `api` → `log-cache`
- MTA: strips system domain from API endpoint
- Rabobank npsb: `api.sys` → `npsb.apps`

This is fragile and assumes a specific URL naming convention.

### 7. V2 API Dependencies Remain

Several actively maintained plugins still depend on V2 CAPI endpoints:
- **stack-auditor:** `/v2/spaces`, `/v2/buildpacks`, `/v2/stacks/{guid}`
- **metric-registrar:** `/v2/user_provided_service_instances`, `/v2/apps/{guid}`
- **service-instance-logs:** V2 service plan/service chain traversal
- **mysql-cli:** go-cfclient V2 for find-bindings workflow

These will break when V2 is disabled and represent the highest migration risk.

---

## Plugins Not Yet Analyzed

The following plugins from https://plugins.cloudfoundry.org/ were not analyzed because
they have not been updated since 2022, are archived, or are no longer actively maintained:

- `andreasf/cf-mysql-plugin` (last push 2023, but no recent activity)
- `alphagov/paas-cf-conduit` (archived 2025)
- `SAP/cf-cli-smsi-plugin` (archived 2025)
- `generalmotors/cf-restage-all` (archived 2023)
- `gemfire/tanzu-gemfire-management-cf-plugin` (archived 2023)
- `armakuni/cf-aklogin` (archived 2021)
- `IBM/cf-icd-plugin` (archived 2017)
- `enric11/cf-cli-check-before-deploy` (last push 2021)
- `dawu415/CF-CLI-Create-Service-Push-Plugin` (last push 2021)
- `homedepot/cf-rolling-restart` (last push 2019)
- `cloudfoundry/noisy-neighbor-nozzle` (archived 2020)
- And many others with last activity before 2022

---

## Implications for the RFC

1. **The minimal core contract (`AccessToken`, `ApiEndpoint`, `IsSSLDisabled`, `GetCurrentOrg`, `GetCurrentSpace`, `Username`, `IsLoggedIn`, `HasSpace`, `HasOrganization`, `HasAPIEndpoint`) covers 100% of actively maintained plugins** that use the plugin API for context/auth.

2. **Domain model methods (`GetApp`, `GetApps`, `GetService`, `GetServices`, `GetOrg`, `GetOrgs`) are used by ~6 plugins** but are being actively migrated away from. The RFC should provide a deprecation path, not immediate removal, for these.

3. **`CliCommandWithoutTerminalOutput` is heavily used** (11/18 plugins), particularly the `"curl"` pattern for CAPI access. The RFC should consider whether to keep this as a transitional mechanism or provide a better alternative (like `CfClient()`).

4. **`CliCommand` is used by 5 plugins** for workflow orchestration (push, bind, restage). Some of these use cases (long-running commands like restage) are known to be unreliable via the plugin API. The RFC should document these limitations.

5. **A standardized `CfClient()` method would eliminate the most common boilerplate** — at least 7 plugins independently bootstrap go-cfclient or custom HTTP clients using the same `AccessToken()` + `ApiEndpoint()` + `IsSSLDisabled()` pattern.
