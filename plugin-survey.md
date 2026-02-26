# CF CLI Plugin Interface Survey

This document captures the results of surveying actively maintained CF CLI plugins
to understand how they use the current plugin interface. The findings inform the
[RFC Draft: CLI Plugin Interface V2](rfc-draft-cli-plugin-interface-v2.md).

**Methodology:** For each plugin, we read the source code directly from GitHub
(via the `gh` CLI API) and performed the following analysis:

1. **Traced every `plugin.CliConnection` call site** — searched for all
   invocations of methods like `AccessToken()`, `GetCurrentSpace()`,
   `CliCommand()`, etc. across the entire codebase.
2. **Examined `go.mod` dependencies** — identified which CF-related libraries
   each plugin depends on (go-cfclient, CF CLI plugin SDK version, loggregator,
   etc.).
3. **Identified non-plugin-API CF interaction** — traced how plugins talk to
   Cloud Foundry (or related platform services) outside the plugin interface:
   go-cfclient V3 typed SDK calls, `CliCommandWithoutTerminalOutput("curl", ...)`
   for CAPI access, custom `net/http` direct calls, `exec.Command("cf", ...)`
   subprocess invocation, or direct `~/.cf/config.json` file I/O.
4. **Read import statements** — identified usage of internal CF CLI packages
   (e.g., `cf/terminal`, `cf/configuration`) that create tight coupling.
5. **Reviewed migration PRs** — where available (e.g., App Autoscaler PR #132),
   examined the before/after to understand what drove the migration.

**Source of plugin list:** Active plugins were identified from
https://plugins.cloudfoundry.org/ filtered to those with GitHub repository
activity since 2022 and not archived, plus community plugins from the
`cloudfoundry-community` and `rabobank` GitHub organizations.

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

### How Plugins Access Cloud Foundry Outside the Plugin Interface

Plugins that need to interact with the Cloud Foundry API (or related platform
services) beyond what the plugin interface provides use one or more of the
following techniques. Many plugins combine multiple techniques.

#### Technique 1: go-cfclient/v3 Library

Plugins bootstrap a [`go-cfclient/v3`](https://github.com/cloudfoundry/go-cfclient)
client using `AccessToken()`, `ApiEndpoint()`, and `IsSSLDisabled()` from the
plugin interface, then use typed SDK methods for CAPI V3 access.

| Plugin | go-cfclient Usage | Library Version |
|---|---|---|
| App Autoscaler | `Applications.ListAll()` — app GUID lookup by name+space | `v3.0.0-alpha.19` |
| DefaultEnv | `Applications.Single()` + `ExecuteAuthRequest()` for `/v3/apps/{guid}/env` | `v3.0.0-alpha.17` |
| cf-lookup-route | `Domains.ListAll()`, `Routes.ListAll()`, `Applications.ListAll()`, `Spaces.GetIncludeOrganization()` | `v3.0.0-alpha.9` |
| Rabobank cf-plugins | Reimplements all V2 model methods via V3; exposes `CfClient()` to consumers | `v3.0.0-alpha.15` |
| mysql-cli (find-bindings) | V2 library: `go-cfclient/v2` for services, plans, bindings, apps, spaces, orgs | `v2` (community fork) |

**How they get credentials:** `AccessToken()` + `ApiEndpoint()` + `IsSSLDisabled()` from the plugin interface, passed to `cfconfig.New()` / `cfclient.New()`.

**Exception — cf-lookup-route:** Reads `~/.cf/config.json` directly via `cfconfig.NewFromCFHome()` instead of using the plugin interface for credentials.

#### Technique 2: `CliCommandWithoutTerminalOutput("curl", "/v3/...")`

Plugins use the plugin interface's `CliCommandWithoutTerminalOutput` method to
run `cf curl` against CAPI endpoints. The CLI handles authentication
automatically. Plugins receive `[]string` (lines of JSON) which they parse
manually into custom structs.

| Plugin | CAPI Endpoints Accessed via `cf curl` |
|---|---|
| stack-auditor | `/v3/apps` (list/patch), `/v3/apps/{guid}/actions/start\|stop`, `/v2/spaces`, `/v2/buildpacks`, `/v2/stacks/{guid}` |
| log-cache-cli | `/v3/apps?guids=...` (bulk resolve), `/v3/service_instances?guids=...` (bulk resolve) |
| metric-registrar | `/v2/user_provided_service_instances`, `/v2/apps/{guid}` |
| service-instance-logs | `/v2/service_plans/{guid}`, `/v2/services/{guid}` (endpoint discovery chain) |
| mysql-cli (migrate) | `/v3/apps`, `/v3/tasks` (create + poll) |
| swisscom appcloud | `/custom/*` (proprietary endpoints), `/v3/audit_events` |
| html5-apps-repo (reads) | `/v3/service_offerings`, `/v3/service_plans`, `/v3/service_instances`, `/v3/service_credential_bindings`, `/v3/apps/{guid}/env` |
| list-services | `/v3/service_bindings?app_guids=...` (paginated) |

**Why `cf curl` instead of direct HTTP?** The CLI handles the `Authorization`
header automatically, so plugins don't need to call `AccessToken()` or manage
TLS configuration. The trade-off is that response headers are not accessible
(which is why html5-apps-repo uses direct HTTP for writes that return `202`
with a `Location` header).

#### Technique 3: Custom Direct HTTP (net/http)

Plugins construct their own `http.Client`, set `Authorization: Bearer <token>`
using `AccessToken()`, configure TLS via `IsSSLDisabled()`, and make raw HTTP
requests to CAPI V3 or service-specific endpoints.

| Plugin | Library / Approach | Endpoints |
|---|---|---|
| upgrade-all-services | Custom `Requester` struct with `jsonry` for JSON | CAPI V3: `/v3/service_plans`, `/v3/service_instances` (GET/PATCH) |
| MTA (MultiApps) | Raw `net/http` with manual URL construction | CAPI V3: `/v3/apps`, `/v3/service_instances`, `/v3/service_credential_bindings`; MTA deploy-service API |
| html5-apps-repo (writes) | Raw `net/http` | CAPI V3: POST/DELETE service instances + service keys; UAA `/oauth/token` |
| spring-cloud-services | Custom `AuthenticatedClient` wrapper | SCS broker: `/cli/instance/{guid}`, `/eureka/apps`, `/actuator/info` |
| OCF Scheduler | `github.com/ess/hype` HTTP library | Scheduler API: `/jobs`, `/calls`, `/schedules` etc. |
| App Autoscaler | Raw `net/http` | Autoscaler API: `/v1/apps/{id}/policy`, `/v1/apps/{id}/scaling_histories` etc. |
| Rabobank consumers | Raw `net/http` | Various service-specific APIs (scheduler, credhub broker, identity broker, network policy) |
| service-instance-logs | `github.com/cloudfoundry/noaa` (WebSocket) | Service instance log streaming endpoint |
| log-cache-cli | `code.cloudfoundry.org/go-log-cache/v3` | Log Cache HTTP API (read, meta, PromQL) |

**How they get credentials:** `AccessToken()` for the bearer token, `ApiEndpoint()` for URL construction, `IsSSLDisabled()` for TLS config — all from the plugin interface.

#### Technique 4: `CliCommandWithoutTerminalOutput` for CLI Command Delegation

Plugins use `CliCommandWithoutTerminalOutput` (or `CliCommand`) to run CF CLI
subcommands as workflow steps, relying on the CLI to handle the full lifecycle
(auth, targeting, API version negotiation, output formatting).

| Plugin | CLI Commands Delegated |
|---|---|
| metric-registrar | `create-user-provided-service`, `bind-service`, `unbind-service`, `delete-service` |
| mysql-cli (migrate) | `push`, `bind-service`, `start`, `delete`, `rename-service`, `create-service-key`, `delete-service-key`, `service-key`, `logs --recent` |
| log-cache-cli | `app <name> --guid`, `service <name> --guid` (GUID resolution) |
| stack-auditor | `stack --guid <name>` (GUID resolution) |
| cf-lookup-route | `target -o <org> -s <space>` (optional re-targeting) |
| MTA | `version` (CLI version detection), `help` (help display) |
| list-services | `help list-services` (help display) |

#### Technique 5: `exec.Command("cf", ...)` — Bypass Plugin API Entirely

Plugins invoke the `cf` CLI binary as a subprocess via `os/exec`, completely
bypassing the plugin RPC interface. This is used as a workaround for plugin API
reliability issues.

| Plugin | What They Run via exec.Command | Why |
|---|---|---|
| cf-java-plugin | `cf ssh`, `cf app --guid`, `cf curl /v3/...`, `cf apps` | `CliConnection.CliCommand("ssh", ...)` had authentication failures that did not occur when running `cf ssh` directly |
| stack-auditor | `cf restage --strategy rolling` | Long-running commands are problematic via the plugin RPC bridge |

#### Technique 6: Direct File I/O on `~/.cf/config.json`

Plugins read the CLI's configuration file directly from disk, bypassing the
plugin interface completely.

| Plugin | What They Read/Write | Why |
|---|---|---|
| cf-targets-plugin | Full `~/.cf/config.json` read/write/copy | Saves and restores complete CLI config snapshots (targets). No plugin API method exists to set or restore configuration. |
| cf-lookup-route | `~/.cf/config.json` read only | Initializes go-cfclient via `cfconfig.NewFromCFHome()` instead of using plugin API methods |

**Why bypass the plugin API?** The plugin interface is read-only for configuration.
There is no `SetApiEndpoint()`, `SetCurrentOrg()`, or `SetAccessToken()`. Plugins
that need to write or restore configuration have no choice but to manipulate the
file directly.

### Technique Usage Summary

| Plugin | go-cfclient | `cf curl` | Direct HTTP | CLI delegation | `exec.Command` | File I/O |
|---|:---:|:---:|:---:|:---:|:---:|:---:|
| OCF Scheduler | - | - | Y (`hype`) | - | - | - |
| App Autoscaler | **V3** | - | Y (autoscaler API) | - | - | - |
| MTA (MultiApps) | - | - | Y (CAPI V3 + deploy svc) | version, help | - | - |
| cf-java-plugin | - | - | - | - | **all** | - |
| cf-targets-plugin | - | - | - | - | - | **all** |
| Rabobank plugins | **V3** | - | Y (service APIs) | - | - | - |
| upgrade-all-services | - | - | Y (CAPI V3) | - | - | - |
| stack-auditor | - | **V2+V3** | - | `stack --guid` | `restage` | - |
| log-cache-cli | - | **V3** (bulk) | Y (log-cache API) | `app/service --guid` | - | - |
| DefaultEnv | **V3** | - | - | - | - | - |
| metric-registrar | - | **V2** | - | `create/bind/unbind/delete-service` | - | - |
| service-instance-logs | - | **V2** | Y (log streaming) | - | - | - |
| spring-cloud-services | - | - | Y (SCS broker) | - | - | - |
| mysql-cli | **V2** | **V3** | - | `push`, `bind`, `restage`, etc. | - | - |
| swisscom appcloud | - | custom+V3 | - | - | - | - |
| html5-apps-repo | - | **V3** (reads) | Y (writes + UAA) | - | - | - |
| cf-lookup-route | **V3** | - | - | `target` (optional) | - | Y (read) |
| list-services | - | **V3** | - | help | - | - |

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

## Version Metadata Limitations

The plugin interface's `VersionType` struct provides only three integer fields:

```go
type VersionType struct {
    Major int
    Minor int
    Build int    // Misnomer: this is SemVer "patch", not build metadata
}
```

The CLI's `PluginVersion.String()` method (in `util/configv3/plugins_config.go`) renders
this as `Major.Minor.Build` or `"N/A"` if all three are zero. There is no support for
SemVer prerelease identifiers (e.g., `-rc.1`, `-beta.2`) or build metadata (e.g.,
`+linux.amd64`, `+20260301`).

### How Plugins Work Around the Limitation

| Plugin | Workaround | Details |
|---|---|---|
| OCF Scheduler | Print full version when run without args | `main()` checks `len(os.Args[1:]) == 0`, prints full SemVer including `SemVerPrerelease` and `SemVerBuild` (set via `-ldflags`), plus build date, VCS URL, VCS commit ID, and VCS commit date |
| cf-targets | Print full version when run without args | Same pattern as OCF Scheduler — prints full SemVer, build date, VCS info, and license notices for embedded libraries |
| App Autoscaler | None observed | Uses only Major/Minor/Build integers |
| MTA (MultiApps) | None observed | Uses only Major/Minor/Build integers |
| Rabobank cf-plugins | Hardcoded user agent version | `userAgent` string is manually set, not derived from `VersionType` |

**Key observations:**
- The `Build` field name is misleading — plugins that use it (e.g., `getVersion("Patch", SemVerPatch)` in OCF Scheduler) map their SemVer patch number to this field, not build metadata.
- Plugins that set version via `-ldflags` (linker variables) at build time can track `SemVerPrerelease` and `SemVerBuild` in code, but cannot pass these to the CLI through the plugin API.
- The information printed by the workaround (run without args) is invisible to `cf plugins` — users cannot see prerelease status or build provenance through the CLI.

---

## Help and Flag Metadata Limitations

### CF CLI Help Guidelines (reference)

The [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines)
define a standardized help format for built-in commands:

1. **NAME** (mandatory) — command name + short description
2. **USAGE** (mandatory) — synopsis following [docopt](http://docopt.org/) conventions (`[]` for optional, `()` for required groups, `|` for mutually exclusive, `...` for repeating)
3. **WARNING** (optional) — critical alerts
4. **EXAMPLE** (optional) — practical usage demonstrations
5. **TIP** (optional) — helpful context or deprecation notices
6. **ALIAS** (optional) — command shortcuts
7. **OPTIONS** (optional) — flag documentation (alphabetical, long option first, defaults appended)
8. **SEE ALSO** (optional) — related commands (comma-separated, alphabetical)

Built-in commands achieve this through Go struct tags on command structs, which the
help system parses at runtime. **Plugins have no equivalent mechanism** — the `Command`
struct supports only `Name`, `Alias`, `HelpText` (single line), and `UsageDetails`
(`Usage` string + `Options map[string]string`).

### CF CLI Style Guide (reference)

The [CF CLI Style Guide](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Style-Guide)
establishes conventions that plugins should follow but cannot fully implement:

- **VERB-NOUN command naming** — plugins can follow this convention for command names
- **Fail-fast validation order** — invalid flags, then prerequisites (logged in, targeted), then server-side resources — plugins must implement this themselves
- **Enum-style flags with values** over multiple boolean flags (e.g., `--strategy rolling` not `--rolling`) — plugins can adopt this pattern, but the `Options map[string]string` cannot express it
- **Output formatting** — tables for lists, key/value for single objects, "OK"/"FAILED" feedback — plugins must implement this independently using `cf/terminal` or their own UI
- **Color conventions** — cyan for resource names, green for "OK", red for "FAILED" — plugins that import `cf/terminal` get this, others reimplement it

### CF CLI Product-Specific Style Guide (reference)

The [CF CLI Product-Specific Style Guide](https://github.com/cloudfoundry/cli/wiki/CLI-Product-Specific-Style-Guide)
adds product-specific conventions that are relevant to plugin consistency:

- **Error message patterns** — standard messages for "not logged in", "no org/space targeted", "not found", "not authorized", "unknown flag". Plugins that implement fail-fast validation should follow these patterns for consistency.
- **TIP messages** — reserved for follow-up commands or required actions, wrapped in single quotes. Plugins cannot emit TIPs through the help system (no `TIP` field in `Command`).
- **Destructive operations** — require confirmation prompts unless `--force` is used. Plugins must implement confirmation prompts independently.
- **Idempotent operations** — create/update/delete should exit 0 if the operation is already in the desired state. Plugins must implement this themselves.
- **Table column ordering** — new columns added at end (not middle) for versioning. Relevant for plugins that output tables.
- **Sensitive data protection** — never output environment variables or labels. Plugins must enforce this independently.

### How `Options map[string]string` Is Processed

The CLI's `ConvertPluginToCommandInfo()` function in `command/common/internal/help_display.go`
converts plugin flag metadata into the internal `CommandFlag` representation:

1. Collects all map keys into a slice and **sorts alphabetically** — plugins cannot control display order
2. Classifies each key by length: **1 character → short flag** (`-f`), **everything else → long flag** (`--force`)
3. Each map entry becomes a **separate** `CommandFlag` with only `Short` OR `Long` set — **never both**
4. The paired rendering path in `FlagWithHyphens()` (`--force, -f`) exists but is **unreachable for plugin flags**
5. The `Default` field on `CommandFlag` is always empty — there is no way to set it through the map

### How Plugins Handle Help and Flags

| Plugin | Options map | Usage string | Notes |
|---|---|---|---|
| OCF Scheduler | **Not used** | Embeds all flag docs in `Usage` string | Complete bypass — `Usage` string contains `--disk[=], -k LIMIT`, `--memory[=], -m LIMIT` etc. with manual formatting. This preserves flag ordering and long/short pairing. |
| cf-targets | Short keys only | Minimal | `"f": "replace the current target..."` — uses 1-char key so it renders correctly as `-f` |
| App Autoscaler | Not observed | Uses `go-flags` | Flag parsing via `github.com/jessevdk/go-flags` — help is handled by the library, not the plugin API |
| MTA (MultiApps) | Not observed | Complex | Multi-line usage strings with embedded flag documentation |
| cf-java-plugin | Not observed | N/A | Uses `github.com/simonleung8/flags` (2017) — flag parsing and help handled by the library |
| Rabobank plugins | Not observed | Minimal | Simple commands with few flags |
| upgrade-all-services | Not observed | Minimal | Few flags, simple usage |
| stack-auditor | Not observed | Minimal | Few flags |
| log-cache-cli | Not observed | Uses `go-flags` | Flag parsing via `github.com/jessevdk/go-flags` |
| mysql-cli | Not observed | Uses `go-flags` | Flag parsing via `github.com/jessevdk/go-flags` |

**Key observations:**

1. **Plugins avoid the `Options` map.** The most feature-rich plugins (OCF Scheduler, MTA) embed flag documentation directly in the `Usage` string rather than use `Options`, because the map loses ordering and cannot pair long/short flag names.

2. **External flag libraries replace the plugin API's flag support.** At least 4 plugins use `github.com/jessevdk/go-flags` and 1 uses `github.com/simonleung8/flags` (unmaintained since 2017). These libraries handle both parsing and help generation, making the plugin API's `Options` map redundant.

3. **No plugin can produce help output matching the CF CLI Style Guide.** The Style Guide specifies: long option listed first with aliases comma-separated, defaults appended as `(Default: value)`, options in alphabetical order. The `map[string]string` API cannot represent paired long/short flags, cannot specify defaults, and while alphabetical sorting is applied, the lack of pairing means `-f` and `--force` appear as separate, unrelated entries.

4. **No plugin can provide EXAMPLE, WARNING, TIP, or SEE ALSO sections.** The Help Guidelines define these as standard help sections for built-in commands, but the plugin `Command` struct has no fields for them.

5. **No plugin help grouping.** All plugin commands appear in a single flat list in `cf help -a`. Built-in commands are organized by category (e.g., "APPS", "SERVICES", "ROUTES"). There is no mechanism for a plugin to declare its command category or for multiple plugins' commands to be grouped by plugin.

---

## Interface Evolution Considerations

### Current CF CLI Version Management

The [CF CLI Version Switching Guide](https://github.com/cloudfoundry/cli/wiki/Version-Switching-Guide)
describes the current approach to CLI version management:

- Different CLI versions (v7, v8) are distributed as **separate binaries** (`cf7`, `cf8`) with symlinks
- Users switch versions by relinking the `cf` symlink or changing `PATH`
- There is **no internal version negotiation** or backward compatibility layer between CLI versions
- On Linux, switching requires full uninstall/reinstall — no side-by-side coexistence

This "separate binaries" approach avoids internal complexity but creates challenges for
plugins that must support multiple CLI versions.

### Implications for Plugin Interface Evolution

The plugin interface should support evolution over time without requiring the
"separate binaries" approach. Key considerations drawn from the CLI's experience:

1. **Plugin `MinCliVersion`** — the existing `MinCliVersion` field in `PluginMetadata` declares the minimum CLI version a plugin requires. The CLI SHOULD warn (not block) when a plugin's `MinCliVersion` exceeds the current CLI version. Currently this field is stored but not meaningfully enforced.

2. **Interface capability negotiation** — rather than version-checking, plugins should be able to discover what capabilities the host CLI provides. This avoids tight version coupling and allows plugins to degrade gracefully on older CLIs (e.g., a plugin could use `CfClient()` if available, or fall back to `AccessToken()` + manual HTTP setup).

3. **Backward-compatible struct evolution** — new fields added to `PluginMetadata`, `Command`, `Usage`, and `PluginVersion` MUST be optional (zero-valued defaults) so that existing compiled plugins continue to work without recompilation. This is why `Flags []FlagDefinition` is proposed alongside (not replacing) `Options map[string]string`.

4. **RPC protocol stability** — the current plugin-to-CLI communication uses Go's `net/rpc` with `encoding/gob` serialization. Adding new methods to the RPC interface is additive and backward-compatible. Changing method signatures or removing methods is breaking. Any future gRPC migration (polyglot plugin support) would need its own version negotiation.

5. **Deprecation signaling** — when the CLI deprecates plugin API methods, it SHOULD emit runtime warnings (not errors) so that plugin users know to request updates from plugin maintainers. This is the pattern used for CF CLI commands themselves (e.g., `cf v3-push` → `cf push`).

---

## Implications for the RFC

1. **The minimal core contract (`AccessToken`, `ApiEndpoint`, `IsSSLDisabled`, `GetCurrentOrg`, `GetCurrentSpace`, `Username`, `IsLoggedIn`, `HasSpace`, `HasOrganization`, `HasAPIEndpoint`) covers 100% of actively maintained plugins** that use the plugin API for context/auth.

2. **Domain model methods (`GetApp`, `GetApps`, `GetService`, `GetServices`, `GetOrg`, `GetOrgs`) are used by ~6 plugins** but are being actively migrated away from. The RFC should provide a deprecation path, not immediate removal, for these.

3. **`CliCommandWithoutTerminalOutput` is heavily used** (11/18 plugins), particularly the `"curl"` pattern for CAPI access. The RFC should consider whether to keep this as a transitional mechanism or provide a better alternative (like `CfClient()`).

4. **`CliCommand` is used by 5 plugins** for workflow orchestration (push, bind, restage). Some of these use cases (long-running commands like restage) are known to be unreliable via the plugin API. The RFC should document these limitations.

5. **A standardized `CfClient()` method would eliminate the most common boilerplate** — at least 7 plugins independently bootstrap go-cfclient or custom HTTP clients using the same `AccessToken()` + `ApiEndpoint()` + `IsSSLDisabled()` pattern.

6. **`VersionType` is insufficient for modern versioning.** Only `Major`/`Minor`/`Build` integers are supported — no prerelease or build metadata. At least 2 plugins work around this by printing the full version when invoked without arguments, making this information invisible to `cf plugins`. The `Build` field name is a misnomer (it's SemVer "patch").

7. **`Options map[string]string` is inadequate for flag documentation.** The most feature-rich plugins bypass it entirely (OCF Scheduler embeds flags in the `Usage` string) or use only single-character keys (cf-targets). At least 5 plugins use external flag parsing libraries that provide their own help generation, making the plugin API's flag support redundant. No plugin can produce help output that conforms to the [CF CLI Help Guidelines](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Help-Guidelines).

8. **Plugin help is second-class compared to built-in commands.** Plugins cannot provide EXAMPLE, WARNING, TIP, or SEE ALSO sections. Plugin commands are not grouped by plugin in `cf help -a`. There is no `cf help <plugin-name>`. The [CF CLI Style Guide](https://github.com/cloudfoundry/cli/wiki/CF-CLI-Style-Guide) establishes conventions that plugins cannot fully implement through the current interface.

9. **Interface evolution should use capability negotiation, not version switching.** The [Version Switching Guide](https://github.com/cloudfoundry/cli/wiki/Version-Switching-Guide) shows the CLI uses separate binaries for major versions. The plugin interface should avoid this pattern by supporting backward-compatible struct evolution (optional new fields) and runtime capability discovery.
