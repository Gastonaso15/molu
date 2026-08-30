# molu — Part 3: The molu Hub

Version: draft-02
Status: pre-implementation
Repository: github.com/ha1tch/molu-hub
Assumes: xolu ≥ v0.15.0 (rename complete, `pkg/client` official, auth machinery exportable)

---

## 1. Scope

This document specifies the **molu hub**: a decoupled catalogue of domain functions that molu frontends consume to expose those functions as MCP tools to agents.

Two things are specified together:

- **The protocol.** The HTTP contract by which domain applications publish functions to the hub and by which molu frontends discover them. This is the normative specification; any implementation that honours it is a valid molu hub.
- **The reference implementation.** A Go binary (`github.com/ha1tch/molu-hub`) that satisfies the protocol and is the intended deployment target in production. Publishers and frontends built against this implementation are the primary consumers on day one.

Out of scope:

- how a domain application decides which of its functions to publish (a product decision, not a hub concern);
- how molu presents discovered functions to the agent (Part 2);
- xolu itself (see the xolu documentation).

The hub is agnostic with respect to vertical application. Function names, descriptions, and schemas pass through the hub unchanged; the hub validates their shape (well-formed JSON, non-empty required fields) but never their meaning.

---

## 2. The three roles

Three roles interact through the hub:

- **Publisher.** A domain application that owns business logic and wishes to expose some of it to agents. Publishes function contracts; renews them periodically; unpublishes on graceful shutdown. Executes function invocations when molu calls the published endpoint.
- **Hub.** Holds the catalogue of registered function contracts. Accepts publishers, expires them on silence, serves consumers. Contains no business logic.
- **Consumer.** Reads the catalogue. molu is the intended consumer in this ecosystem; the protocol does not exclude other consumers, though none are specified here.

The three roles are strictly separated. The hub never invokes a published function; molu invokes it directly, using the endpoint URL the publisher registered. The hub never talks to the substrate on the publisher's behalf; the publisher owns its own xolu connection. The consumer never talks to the publisher through the hub; the hub only mediates catalogue *discovery*, never invocation.

---

## 3. Deployment topology

**One hub per tenant.** A molu frontend serves exactly one tenant (see Part 2 §2); its paired hub also serves exactly one tenant. Publishers registering with a hub belong to that tenant. Consumers reading the catalogue see functions scoped to that tenant. There is no cross-tenant catalogue view, no shared hub across tenants, no tenant filtering at read time — the tenant boundary is the process boundary.

A typical deployment for one tenant is therefore three processes running together:

- one xolu instance (the substrate);
- one molu hub (the catalogue);
- one molu frontend (the MCP server the agent talks to).

The domain application — the publisher — is a fourth process (or set of processes) that also belongs to this tenant. It talks to xolu directly for its own business logic and to the molu hub to publish functions.

Different tenants run separate stacks. There is no shared infrastructure between tenants at any layer.

---

## 4. Function contract

A registered function is described by a **contract** — a JSON document that fully specifies what the function is called, what it accepts, what it returns, and how molu should invoke it.

### 4.1. Contract fields

```json
{
  "namespace": "example-app",
  "name": "CreateOrder",
  "description": "Create a new order with the given items.",
  "input_schema":  { "type": "object", "properties": { "..." } },
  "output_schema": { "type": "object", "properties": { "..." } },
  "endpoint":      "https://example-app.internal/agent/create-order",
  "auth": {
    "mode":       "bearer",
    "header":     "Authorization",
    "token_ref":  "vault:example-app-agent-token"
  },
  "requires_confirmation": true,
  "idempotent":            false,
  "cost":                  "moderate",
  "latency":               "sub-second"
}
```

Field semantics:

- **`namespace`** — the publisher's namespace. Combined with `name`, forms the tool name molu registers with the agent (`example-app.CreateOrder`). A publisher may only register under namespaces its credential authorises (see §5).
- **`name`** — the function's unqualified name. Combined with `namespace` to produce the MCP tool name.
- **`description`** — human-readable description, passed verbatim to the agent as the MCP tool description. This is what the agent reads to understand when to call the function.
- **`input_schema`** — JSON Schema for the function's input, passed verbatim to molu, which passes it verbatim to the agent as the tool's `inputSchema`. Must be a valid JSON Schema document; the hub rejects malformed schemas at registration.
- **`output_schema`** — optional JSON Schema for the function's output. When present, molu forwards it as the tool's `outputSchema`.
- **`endpoint`** — the absolute HTTP URL molu invokes to execute the function. The publisher owns this endpoint and is responsible for its availability, authentication, and behaviour.
- **`auth`** — how molu authenticates when invoking. `mode` is one of `none`, `bearer`, `apikey`; `header` is the HTTP header name (defaults to `Authorization` for bearer); `token_ref` is a reference to the secret molu will resolve at invocation time. Secrets are never stored in the hub itself — the hub stores only the reference.
- **`requires_confirmation`** — if `true`, molu surfaces an MCP elicitation to the agent (which typically routes to a human confirmation) before invoking. Used for destructive or irreversible operations.
- **`idempotent`** — if `true`, molu may safely retry the invocation on transient failures. If `false`, molu never retries.
- **`cost`** — a qualitative marker (`low`, `moderate`, `high`) hinting at the resource or business cost of invoking the function. Passed to molu as tool metadata; the agent may use it to reason about invocation prudence.
- **`latency`** — a qualitative marker (`sub-second`, `seconds`, `minutes`) hinting at the expected response time. Passed as tool metadata; useful for agent planning.

### 4.2. Function versioning

There is no function versioning in draft-02. Every registered function is implicitly version 1. Re-registering a function with the same `namespace` and `name` replaces the previous contract atomically; consumers see the new contract on their next catalogue poll. Publishers are responsible for not breaking their consumers with incompatible changes; the hub provides no compatibility checking.

Versioning may be added in a future revision if operational experience shows a need.

### 4.3. Contract validation at registration

The hub validates a submitted contract at registration:

- `namespace` and `name` are non-empty and match a permitted character set (`[a-zA-Z][a-zA-Z0-9_-]*`).
- `namespace` is in the publisher's authorised set (see §5).
- `description` is non-empty.
- `input_schema` is valid JSON Schema (draft 2020-12 or later).
- `output_schema`, when present, is valid JSON Schema.
- `endpoint` is a well-formed absolute URL with an `http` or `https` scheme.
- `auth.mode` is one of the allowed values; if `bearer` or `apikey`, `token_ref` is non-empty.
- `requires_confirmation` and `idempotent` are booleans.
- `cost` is one of `low`, `moderate`, `high`; `latency` is one of `sub-second`, `seconds`, `minutes`.

Any validation failure returns HTTP 400 with a diagnostic listing the offending fields. The hub does not attempt partial acceptance.

---

## 5. Authentication and namespaces

The hub uses the same three authentication modes as xolu: bearer, API key, and JWT. The reference implementation imports xolu's auth machinery (`pkg/middleware/auth` and its supporting packages) directly rather than reimplementing it. If those packages are not cleanly exportable at the time of hub implementation, extracting them is a prerequisite task recorded against xolu, not against the hub.

### 5.1. Credentials and namespaces

Each credential the hub accepts is bound to:

- a **tenant** — always the hub's own tenant, since one hub serves one tenant;
- a **role** — `publisher`, `consumer`, or both;
- a **namespace set** — for publishers, the namespaces under which the credential may register functions.

The namespace set is declared out-of-band, as a hub configuration entry keyed by credential identifier. For JWT-based publishers, a signed `namespaces: [...]` claim is honoured directly, matching the pattern xolu uses for `tenants: [...]` claims.

### 5.2. Namespace enforcement

A publisher submitting a function contract whose `namespace` field is not in the credential's authorised set is rejected with HTTP 403 and diagnostic `MOLU-HUB-NS001`. Publishers can enumerate their own permitted namespaces via `GET /whoami`.

Consumers do not have namespaces; a consumer credential can read the entire catalogue for its tenant.

---

## 6. Publisher protocol

Publishers interact with the hub via four HTTP endpoints. All requests carry the credential in the standard Authorization header per the auth mode.

### 6.1. `POST /publish`

Register or re-register one function.

Request body: a function contract (§4.1).

Response:

- **200 OK** if the function was registered (either newly or updated in place). Response body echoes the accepted contract with a hub-assigned `registered_at` timestamp.
- **400 Bad Request** if the contract fails validation. Response body lists the offending fields.
- **403 Forbidden** if the credential is not a publisher or the namespace is not authorised.

Re-registration is atomic: the new contract replaces the old with no interim state where the function is missing.

### 6.2. `POST /heartbeat`

Renew the publisher's liveness. All functions from a publisher expire together if no heartbeat is received within the timeout window.

Request body: empty, or optionally a JSON array of function fully-qualified names the publisher wishes to confirm are still owned by this credential (a consistency check; the hub verifies and returns any discrepancies).

Response:

- **200 OK** with the current count of functions registered by this publisher and the seconds remaining before expiry if no further heartbeats arrive.

Publishers must heartbeat at `MOLU_HUB_HEARTBEAT_INTERVAL` (default 30 seconds). The hub expires publisher registrations after `MOLU_HUB_HEARTBEAT_TIMEOUT` (default 90 seconds) of silence — three missed heartbeats at the default interval.

### 6.3. `POST /unpublish`

Remove one or all of the publisher's functions.

Request body: either a JSON array of function fully-qualified names to remove, or an empty body to remove all functions owned by this credential.

Response:

- **200 OK** with the list of removed function names.

Unpublishing is atomic per-function: a function is either fully present in the catalogue or fully absent, never partially removed.

### 6.4. `GET /whoami`

Return the calling credential's identity, role, and authorised namespaces. Useful for publisher diagnostics.

Response:

```json
{
  "role":       ["publisher"],
  "namespaces": ["example-app", "example-app-experimental"],
  "tenant":     "t-1234"
}
```

---

## 7. Consumer protocol

Consumers (molu frontends and any other reader) interact with the hub via three endpoints. All requests carry the credential.

### 7.1. `GET /catalogue`

List all currently-registered functions.

Response:

```json
{
  "functions": [
    { "namespace": "example-app", "name": "CreateOrder", "description": "...", "..." },
    { "namespace": "example-app", "name": "CancelOrder", "description": "...", "..." }
  ],
  "generated_at": "2026-07-17T14:30:00Z"
}
```

The returned list is a point-in-time snapshot. A function that expires between two consecutive consumer polls will simply be absent from the next response.

Query parameters:

- **`namespace`** — restrict to functions under the given namespace.
- **`name`** — restrict to functions whose name matches (exact match; no wildcards).

### 7.2. `GET /catalogue/{namespace}`

Convenience endpoint equivalent to `GET /catalogue?namespace={namespace}`.

### 7.3. `GET /catalogue/{namespace}/{name}`

Retrieve one specific function contract by fully-qualified name.

Response:

- **200 OK** with the function contract as a single JSON object.
- **404 Not Found** if no function with that fully-qualified name is currently registered.

---

## 8. Storage

The reference implementation supports two storage backends, selected at startup:

### 8.1. Memory (default)

`MOLU_HUB_STORAGE=memory`

The catalogue lives in a `sync.Map`-backed in-process structure. All registrations are lost on process restart. Publishers must re-register on reconnect, which they will do anyway as part of their normal startup handshake.

This is the recommended default. It is simple, fast, and has the useful property that a hub restart forces publishers to re-declare themselves — no stale contracts from long-dead publishers can survive.

### 8.2. xolu-backed (optional)

`MOLU_HUB_STORAGE=xolu`

The catalogue is stored as entities in xolu. Each function contract is one entity of type `molu_hub_function`, keyed by fully-qualified name. Registration writes an entity; unpublishing deletes it; expiry deletes it. Consumer reads translate to xolu entity list queries.

Configuration when using xolu-backed storage:

```
MOLU_HUB_STORAGE=xolu
MOLU_HUB_XOLU_URL=http://localhost:8080
MOLU_HUB_XOLU_AUTH_MODE=jwt
MOLU_HUB_XOLU_TOKEN_FILE=/etc/molu-hub/xolu.jwt
```

This mode is useful when the hub itself needs to survive restarts with its catalogue intact, or when operators want the catalogue to be inspectable through the same tooling they use for other xolu-backed data. The trade-off is a bootstrap dependency: the hub cannot start until xolu is reachable.

The hub uses `github.com/ha1tch/xolu/pkg/client` for its xolu interactions, the same client molu uses. Auth modes are the same three (bearer, API key, JWT).

### 8.3. Expiry behaviour under xolu-backed storage

Publisher expiry deletes the corresponding entities from xolu, just as unpublishing does. There is no distinction in the catalogue between "publisher went silent" and "publisher explicitly unpublished" — both result in absence.

---

## 9. Configuration

The complete configuration surface of the reference implementation, environment-variable form:

```
# Required
MOLU_HUB_ADDR                     HTTP listen address (default :9080)
MOLU_HUB_TENANT                   tenant identifier this hub serves

# Auth
MOLU_HUB_AUTH_MODE                bearer | apikey | jwt (mixed allowed)
MOLU_HUB_JWT_SECRET_FILE          path to JWT signing secret (jwt mode)
MOLU_HUB_APIKEYS_FILE             path to API keys and grants (apikey mode)
MOLU_HUB_BEARER_TOKEN_FILE        path to bearer token (bearer mode)

# Storage
MOLU_HUB_STORAGE                  memory | xolu (default memory)
MOLU_HUB_XOLU_URL                 xolu URL when MOLU_HUB_STORAGE=xolu
MOLU_HUB_XOLU_AUTH_MODE           bearer | apikey | jwt
MOLU_HUB_XOLU_TOKEN_FILE          credential file for hub-to-xolu auth

# Publisher liveness
MOLU_HUB_HEARTBEAT_INTERVAL       30s  (expected publisher heartbeat cadence)
MOLU_HUB_HEARTBEAT_TIMEOUT        90s  (publisher expires after this silence)
MOLU_HUB_EXPIRY_CHECK_INTERVAL    15s  (how often the hub scans for expired publishers)

# TLS
MOLU_HUB_TLS_CERT_FILE            path to TLS cert (optional; empty for plain HTTP)
MOLU_HUB_TLS_KEY_FILE             path to TLS key

# Observability
MOLU_HUB_LOG_LEVEL                info | debug | warn | error
MOLU_HUB_LOG_FORMAT               console | json
MOLU_HUB_METRICS_ADDR             :9091 (Prometheus /metrics; empty to disable)
```

All settings also accept a config file (`--config /path/to/molu-hub.yaml`). Environment variables override file values. Command-line flags override both. Secrets are never read from command-line flags.

---

## 10. Reference implementation architecture

The reference implementation is a single Go binary organised as follows:

```
molu-hub/
├── cmd/molu-hub/            entry point, config, startup
├── pkg/api/                 HTTP handlers for /publish, /heartbeat, /catalogue, etc.
├── pkg/store/               storage backends: memory, xolu
├── pkg/auth/                thin wrapper over xolu's auth machinery
├── pkg/liveness/            heartbeat tracking, expiry sweep
├── pkg/config/              configuration
└── pkg/obs/                 observability
```

The dependency graph is acyclic. `api` depends on `store`, `auth`, `liveness`. `store` and `auth` and `liveness` do not depend on each other. `config` and `obs` are leaves.

Target Go version: 1.25. Structured logging via `log/slog`. Third-party dependencies limited to the xolu client, xolu's exported auth packages, and the standard library.

---

## 11. Observability

The reference implementation exposes:

- **`/healthz`** — returns 200 if the process is running; 503 if the storage backend is unreachable (for `xolu` mode).
- **`/readyz`** — returns 200 once the initial catalogue is loaded (from xolu on startup) or immediately on `memory` mode.
- **`/metrics`** — Prometheus text format at the address configured by `MOLU_HUB_METRICS_ADDR`. Metrics include: total registered functions, functions per namespace, publisher count, heartbeat rate, catalogue read rate, expiry event count.

Structured log lines carry at minimum: timestamp, level, component (`api`, `store`, `liveness`), operation, credential identifier (never the credential itself), outcome. No function payloads are logged — the hub is not on the invocation path, so it never sees them anyway.

---

## 12. Availability behaviour

The hub is a single process. If it goes down, molu frontends serving that tenant lose access to domain functions; generic primitives against xolu continue to work (see Part 2 §11).

If storage is `memory`, restart clears the catalogue and publishers must re-register. Publishers must therefore be designed to detect connection loss and re-register on reconnect. The reference publisher pattern is:

1. On startup, register all functions.
2. Start a heartbeat goroutine.
3. On any HTTP error against the hub, retry with backoff.
4. On a heartbeat that returns 401 (credential rejected) or 404 (function not found), re-register all functions and resume heartbeating.

If storage is `xolu`, restart preserves the catalogue but publishers still heartbeat; expired publishers still have their functions removed. The hub's own reachability of xolu is a hard dependency in this mode — an unreachable xolu means the hub cannot serve consumer requests until connectivity is restored.

The hub has no built-in high-availability: no clustering, no leader election, no replication. Running multiple hubs behind a load balancer is not supported by this specification — publisher heartbeats would be spread across instances and produce inconsistent expiry. If HA becomes a requirement, it will be addressed in a future revision.

---

## 13. Future work

Recorded so a reader knows what is *not* in draft-02.

**Function versioning.** Explicit version fields on contracts, allowing multiple versions of the same function to coexist and consumers to negotiate. Deferred until operational experience shows a concrete need.

**Push-based consumer updates.** Currently consumers poll. A future revision may add server-sent-events or an equivalent mechanism for consumers who want real-time catalogue updates without polling.

**Hub-side function invocation.** In the current design molu invokes functions directly at the publisher's endpoint. A future revision could route invocations through the hub, allowing centralised authentication, rate limiting, and observability at the cost of an extra hop and a new failure mode. Not planned for draft-02; the direct-invocation design is simpler and gives publishers full control over their endpoints.

**High availability.** No clustering in draft-02. If HA becomes necessary, the natural design is leader-elected replication with publishers heartbeating to any instance and the leader owning expiry. Deferred.

**Cross-hub federation.** Multiple hubs (one per tenant, per §3) do not currently share information. A future revision might allow a molu frontend to consult multiple hubs — for example, a tenant-specific hub for private functions and a shared hub for platform-wide functions. Deferred; the current design intentionally makes the hub a per-tenant object.

---

*Draft 02. Pre-implementation. Assumes xolu ≥ v0.15.0.*
