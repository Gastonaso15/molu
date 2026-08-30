# molu — Part 2: The MCP Frontend

Version: draft-02
Status: pre-implementation
Repository: github.com/ha1tch/molu
Assumes: xolu ≥ v0.15.0 (rename complete, `pkg/client` official, `cal` HTTP endpoints available)

---

## 1. Scope

This document specifies the internal architecture of the molu process — the MCP server that fronts a xolu instance and, when configured, consumes function contracts from a molu hub. It defines what molu must implement, what contracts it must honour, and what behaviour it must exhibit under normal and degraded conditions.

It does **not** specify:

- the molu hub (see Part 3);
- the domain application publishing functions to the hub (out of scope for the molu project entirely);
- the xolu substrate (see the xolu documentation).

molu is agnostic with respect to vertical application. No domain vocabulary — invoicing, inventory, custody, workflow, or any other — appears in molu's code, tool descriptions, error messages, or documentation. Domain vocabulary enters molu only as strings passed through from the xolu schema or from the hub's function catalogue.

---

## 2. Runtime shape

molu is a single Go binary, stateless, that opens two long-lived connections:

- one to a **xolu** instance, via `github.com/ha1tch/xolu/pkg/client`;
- one to an **MCP client** (the agent), via the official Go SDK `github.com/modelcontextprotocol/go-sdk`.

When configured, molu additionally opens a third connection:

- one to a **molu hub**, to discover published domain functions.

No persistent storage. No entity-data or query-result cache — xolu owns caching for the read surface (LRU or Redis behind the `pkg/cache.Cache` interface, invalidated by xolu on writes). The one thing molu holds in memory is the **semantic map**: the schemas, FSM definitions, generators, and event definitions that describe the substrate's operational shape. This is molu's operating context, not a cache; it is refreshed by polling the xolu schema endpoints because xolu does not emit schema-change events.

Target Go version: 1.25. Structured logging via `log/slog`. No cgo dependencies beyond what xolu and its client transitively require.

**Tenant boundary.** One molu process serves exactly one tenant. The tenant is fixed by the credential molu uses to authenticate to xolu; there is no per-request tenant negotiation. Cross-tenant work — comparative reporting across tenants, for instance — is achieved by running one molu instance per tenant with distinct MCP clients, or by publishing a domain function to the hub that internally aggregates across tenants under a service credential.

---

## 3. Component architecture

```
molu/
├── cmd/molu/           entry point, config, startup
├── pkg/mcp/            MCP server: tool registration, dispatch, transport
├── pkg/semantic/       semantic map: schema reader, resolver, refresh
├── pkg/catalogue/      hub client: function discovery, refresh
├── pkg/exec/           execution: xolu client wrapper, plan validation
├── pkg/config/         configuration
└── pkg/obs/            observability: logging, metrics, tracing hooks
```

Interaction between components is strictly one-directional along the request path: `mcp → semantic → exec → xolu client`. Refresh flows independently: a schema poller updates `semantic`; a catalogue poller updates `catalogue`. The dependency graph is acyclic.

---

## 4. The semantic map

The semantic map is molu's internal representation of the xolu instance it is connected to. It is the substrate for every MCP tool that molu exposes and for every plan molu validates before executing.

### 4.1. Sources

The map is built from four xolu subsystems, read at startup and refreshed on the schema poll interval:

- **Entity schemas** via the schema listing route (through the client). For each registered entity type: field names and types, REF fields (fields whose value is a reference to another entity), and the raw JSON Schema for validation of writes.
- **FSM definitions** via `GET /api/v2/fsm/def`. For each definition: states, transitions (from-state, input, to-state, guard expression, set clauses, Mealy output), and the definition's declared machine variables.
- **Generators and sequences** via `GET /api/v2/gen/{type}` and `GET /api/v2/gen/seq`. For each: name, kind (sequence, ULID, CUID, UUID, and so on), and any exposed parameters.
- **Event definitions** via `GET /api/v2/event/def`. For each: event type, latch source, and target action.

### 4.2. Structure

```go
type SemanticMap struct {
    Entities   map[string]*EntityDef   // by name
    Machines   map[string]*MachineDef  // by definition id
    Sequences  map[string]*GenDef      // by name
    Events     map[string]*EventDef    // by id
    Tenant     string                  // set at connection scope
    ReadAt     time.Time
}

type EntityDef struct {
    Name       string
    Schema     json.RawMessage        // as returned by xolu
    Fields     []FieldDef
    Refs       []RefFieldDef          // subset of Fields with format:ref
    FSMs       []string               // ids of definitions bound to this type
}

type MachineDef struct {
    ID           string
    EntityType   string                // may be empty for entity-agnostic FSMs
    InitialState string
    States       map[string]StateDef
    Transitions  []TransitionDef
    Variables    []VariableDef
}

type TransitionDef struct {
    From    string
    Input   string
    To      string
    Guard   string    // T-SQL expression, empty if unconditional
    SetOps  []string  // T-SQL SET clauses
    Output  string    // Mealy output expression, empty if none
}
```

The map is held under a `sync.RWMutex`. Refresh replaces the map atomically; readers hold a read lock for the duration of a single MCP tool call.

### 4.3. Refresh

molu polls the xolu schema endpoints every `MOLU_FRONT_SCHEMA_POLL_INTERVAL` (default 60s) to detect changes to schemas, FSM definitions, generators, and event definitions. xolu does not emit events for these mutations, so polling is the mechanism — not a fallback.

Refresh is atomic: the poller builds the new map into a fresh structure and swaps it in under the write lock. Concurrent MCP tool calls see either the old map completely or the new map completely, never a partial state. A failed poll (network error, xolu unreachable) is logged at `warn` level and the current map is retained; the next poll retries.

### 4.4. Resolution

The semantic map answers questions like:

- Given the string `"customer"`, what entity type is meant? (Case-insensitive lookup.)
- Given a field name mentioned by the agent, does it exist on the entity? What is its type? Is it a REF?
- Given an FSM transition, what does its guard require, and what will its set clauses compute? (Read from `TransitionDef.Guard` and `TransitionDef.SetOps` verbatim; the agent gets the T-SQL text and may reason about it.)

Resolution is **structural**, not statistical. molu does not do natural-language matching against entity names or field descriptions. The agent is expected to use the `describe` tool to read the schema and construct its calls against exact names.

---

## 5. The MCP tool surface

molu exposes two categories of MCP tools: **generic primitives** derived from the semantic map, and **domain functions** discovered from the hub. Both categories appear in the same tool list to the agent, distinguished by namespace: generic primitives are unnamespaced (`get`, `walk`, and so on); domain functions carry the publishing application's namespace (`example-app.CreateOrder`).

### 5.1. Generic primitives

Thirteen tools. Each is specified with its MCP schema below.

#### 5.1.1. `describe`

Read the semantic map. The agent's principal orientation tool.

```json
{
  "name": "describe",
  "description": "Return xolu's operational schema: entity types, FSM definitions, generators, event definitions.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "scope": {
        "type": "string",
        "enum": ["all", "entities", "machines", "generators", "events"],
        "default": "all"
      },
      "name": {
        "type": "string",
        "description": "Optional. Restrict to one entity type, machine definition, generator, or event definition by name."
      }
    }
  }
}
```

Returns the corresponding slice of the semantic map, serialised as JSON.

#### 5.1.2. `get`

Retrieve one entity by type and id.

```json
{
  "name": "get",
  "description": "Retrieve a specific entity by type and id.",
  "inputSchema": {
    "type": "object",
    "required": ["entity_type", "id"],
    "properties": {
      "entity_type": { "type": "string" },
      "id":          { "type": ["string", "integer"] }
    }
  }
}
```

Dispatches to `GET /api/v1/{entity_type}/{id}` via the xolu client.

#### 5.1.3. `list`

List entities of a type, with optional filters. Pass-through to xolu; molu does not disambiguate, sort, or post-filter results.

```json
{
  "name": "list",
  "description": "List entities of a given type, optionally filtered.",
  "inputSchema": {
    "type": "object",
    "required": ["entity_type"],
    "properties": {
      "entity_type": { "type": "string" },
      "filter":      { "type": "object", "description": "Field-value equality filters." },
      "limit":       { "type": "integer", "default": 50, "minimum": 1, "maximum": 500 },
      "offset":      { "type": "integer", "default": 0, "minimum": 0 }
    }
  }
}
```

Dispatches to `GET /api/v1/{entity_type}` with query parameters.

#### 5.1.4. `create`

Create one entity of a given type.

```json
{
  "name": "create",
  "description": "Create a new entity.",
  "inputSchema": {
    "type": "object",
    "required": ["entity_type", "data"],
    "properties": {
      "entity_type": { "type": "string" },
      "data": { "type": "object", "description": "Field values for the new entity." }
    }
  }
}
```

Dispatches to `POST /api/v1/{entity_type}`. molu validates `data` against the entity's JSON Schema from the semantic map **before** issuing the request; a schema violation is returned as an MCP tool error with the offending fields listed.

#### 5.1.5. `update`

Update fields on one entity.

```json
{
  "name": "update",
  "description": "Update fields on an existing entity.",
  "inputSchema": {
    "type": "object",
    "required": ["entity_type", "id", "changes"],
    "properties": {
      "entity_type": { "type": "string" },
      "id":          { "type": ["string", "integer"] },
      "changes":     { "type": "object" },
      "version":     { "type": "integer", "description": "Optimistic concurrency version." }
    }
  }
}
```

Dispatches to `PATCH /api/v1/{entity_type}/{id}`. molu validates `changes` against the entity's JSON Schema.

#### 5.1.6. `query`

Execute an OQL or Sulpher query directly. Available when the schema is insufficient and the agent needs a structured query it can construct itself.

```json
{
  "name": "query",
  "description": "Execute a query against xolu. Use OQL for tabular results, Sulpher for graph patterns.",
  "inputSchema": {
    "type": "object",
    "required": ["language", "query"],
    "properties": {
      "language": { "type": "string", "enum": ["oql", "sulpher"] },
      "query":    { "type": "string" },
      "params":   { "type": "object", "description": "Optional bind parameters." }
    }
  }
}
```

Dispatches through the xolu client's query surface. molu does **not** parse or validate the query; xolu owns that responsibility.

#### 5.1.7. `walk`

Advance an FSM machine by input, with optional payload.

```json
{
  "name": "walk",
  "description": "Advance a state machine by input. The machine's guards will evaluate against variables and payload; the transition either applies or is rejected with the guard's diagnostic.",
  "inputSchema": {
    "type": "object",
    "required": ["machine_id", "input"],
    "properties": {
      "machine_id": { "type": ["string", "integer"] },
      "input":      { "type": "string" },
      "payload":    { "type": "object", "description": "Fields available to guards as payload.<field>." }
    }
  }
}
```

Dispatches to `POST /api/v2/fsm/machine/{id}/walk`. The response includes the resulting state, any Mealy output, and (on rejection) the guard diagnostic — all passed through from xolu to the agent verbatim.

#### 5.1.8. `machine_state`

Read a machine's current state and variables.

```json
{
  "name": "machine_state",
  "description": "Read current state, variables, and terminal status of a machine.",
  "inputSchema": {
    "type": "object",
    "required": ["machine_id"],
    "properties": {
      "machine_id": { "type": ["string", "integer"] }
    }
  }
}
```

Dispatches to `GET /api/v2/fsm/machine/{id}/state`. Also reads `/vars` and merges. This is a separate tool from `get` because the machine has a first-class current-state concept and dedicated variables that are not entity fields.

#### 5.1.9. `machine_history`

Read a machine's transition history.

```json
{
  "name": "machine_history",
  "description": "Read the transition history of a machine.",
  "inputSchema": {
    "type": "object",
    "required": ["machine_id"],
    "properties": {
      "machine_id": { "type": ["string", "integer"] },
      "limit":      { "type": "integer", "default": 20, "minimum": 1, "maximum": 500 }
    }
  }
}
```

Dispatches to `GET /api/v2/fsm/machine/{id}/history`.

#### 5.1.10. `cal_check`

Dry-run availability check against one or more calendars over a proposed span, without booking.

```json
{
  "name": "cal_check",
  "description": "Check availability against one or more calendars for a proposed time span, without creating a booking.",
  "inputSchema": {
    "type": "object",
    "required": ["calendars", "start", "end"],
    "properties": {
      "calendars": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Calendar identifiers to intersect."
      },
      "start": { "type": "string", "format": "date-time" },
      "end":   { "type": "string", "format": "date-time" }
    }
  }
}
```

Dispatches through the xolu client's `cal` check surface. Returns availability and, on unavailability, the blocking diagnostic naming the calendars that killed the intersection.

#### 5.1.11. `cal_openings`

Find open windows across one or more calendars.

```json
{
  "name": "cal_openings",
  "description": "Find available windows across one or more calendars, given a duration and search range.",
  "inputSchema": {
    "type": "object",
    "required": ["calendars", "duration", "from", "to"],
    "properties": {
      "calendars": {
        "type": "array",
        "items": { "type": "string" }
      },
      "duration": { "type": "string", "description": "ISO 8601 duration (e.g. PT1H)." },
      "from":     { "type": "string", "format": "date-time" },
      "to":       { "type": "string", "format": "date-time" },
      "objective": {
        "type": "string",
        "enum": ["earliest", "first-fit", "emptiest", "longest-clear-margin"],
        "default": "earliest"
      },
      "limit":    { "type": "integer", "default": 10, "minimum": 1, "maximum": 100 }
    }
  }
}
```

Dispatches through the xolu client's `cal` openings surface.

#### 5.1.12. `cal_propose`

Create a proposed booking (proposed plane; not yet confirmed).

```json
{
  "name": "cal_propose",
  "description": "Create a proposed booking on one or more calendars. Bookings are placed on the proposed plane until confirmed.",
  "inputSchema": {
    "type": "object",
    "required": ["calendars", "start", "end"],
    "properties": {
      "calendars": {
        "type": "array",
        "items": { "type": "string" }
      },
      "start": { "type": "string", "format": "date-time" },
      "end":   { "type": "string", "format": "date-time" },
      "payload": { "type": "object", "description": "Optional application-defined metadata for the booking." }
    }
  }
}
```

Dispatches through the xolu client's `cal` propose surface. Returns a booking id.

#### 5.1.13. `cal_confirm`

Move a proposed booking to the binding plane.

```json
{
  "name": "cal_confirm",
  "description": "Confirm a previously proposed booking, moving it from the proposed plane to the binding plane.",
  "inputSchema": {
    "type": "object",
    "required": ["booking_id"],
    "properties": {
      "booking_id": { "type": ["string", "integer"] }
    }
  }
}
```

Dispatches through the xolu client's `cal` confirm surface.

### 5.2. Domain functions

For every function in the hub catalogue, molu registers an MCP tool with:

- **name**: the function's namespaced name from the hub (e.g. `example-app.CreateOrder`).
- **description**: the function's description from the hub, verbatim.
- **inputSchema**: the function's input JSON Schema from the hub, verbatim.
- **outputSchema** (when supplied): the function's output JSON Schema from the hub, verbatim.

Invocation is a passthrough. molu receives the agent's call, validates against the input schema, and forwards to the function's endpoint as declared in the hub contract. The response is returned to the agent unchanged.

molu adds no interpretation, no side effects, no reasoning. If the function requires human confirmation per its hub metadata, molu surfaces that as an MCP elicitation request before invoking.

molu does **not** cache domain-function responses. Idempotency, retries, and side-effect semantics are properties of the function itself, declared in its hub metadata.

### 5.3. Namespace collisions

If a hub-published function has a name that collides with a generic primitive (`get`, `walk`, `cal_check`, and so on), molu refuses to register the domain function and logs an error. Generic primitives are always unnamespaced and always win. Publishers should use namespaced function names in the hub as a matter of policy; this is enforced at the hub in Part 3.

---

## 6. Execution

Every tool call goes through `pkg/exec`, which is a thin layer over the xolu client that adds:

- **Read-lock acquisition on the semantic map** for the duration of the call.
- **Input validation** against the semantic map (entity type exists; field names are declared; FSM machine id exists in the catalogue).
- **JSON Schema validation** of write payloads against the entity's declared schema.
- **Uniform error mapping** from xolu error codes to MCP tool errors, preserving the `XOLU-*` code as a structured field so the agent can reason about the failure type.
- **Slog structured logging** of every call: tool name, tenant, entity type or machine id, duration, outcome. No payload contents at info level; payloads redacted or dropped at debug level according to config.

`pkg/exec` does **not** implement retries. The xolu client owns retry policy for its own transport-layer failures. molu-level retries would violate the substrate's semantics for state-modifying operations.

---

## 7. Refresh

Two independent refresh loops:

**Schema refresh.** Polls the xolu schema endpoints (entities, FSM definitions, generators, event definitions) every `MOLU_FRONT_SCHEMA_POLL_INTERVAL` (default 60s). Builds a fresh semantic map; swaps atomically under the write lock. Failed polls are logged at `warn` and the current map is retained.

**Catalogue refresh.** When the hub is configured, polls the hub's function catalogue every `MOLU_FRONT_CATALOGUE_POLL_INTERVAL` (default 60s). Rebuilds the registered domain-function tool set atomically. Newly added functions become available to the agent on the next MCP `tools/list` invocation; removed functions become unavailable. Namespace collisions with generic primitives are logged and the offending function is skipped. Failed polls are logged and the current catalogue is retained.

Neither loop involves the xolu event stream. molu registers no event subscriptions in draft-02.

The schema refresh loop is gated by the xolu health probe (see §8). A poll only issues its HTTP calls when the probe holds a fresh successful pong.

---

## 8. xolu health probe

molu maintains a background probe against xolu. Every tool call, and every scheduled poll, is gated on the probe holding a fresh successful pong. The purpose is twofold: to avoid adding load to a xolu that is struggling, and to give the agent a fast, honest failure signal when the substrate is not available.

### 8.1. Probe mechanism

The probe issues `GET /ready` against xolu, using the xolu client. xolu's `/ready` endpoint returns 200 when the process is initialised and its storage layer's `Ping` succeeds; it returns 503 during initialisation or when storage is unreachable. This is the endpoint designed for exactly this purpose.

The probe runs on its own goroutine, independent of any MCP tool call. It records:

- `LastPongAt`: timestamp of the last successful pong.
- `LastFailAt`: timestamp of the last failed probe.
- `Healthy`: derived boolean — true when `LastPongAt` is within `MOLU_FRONT_PONG_FRESHNESS` of now and no unresolved failure has intervened.

### 8.2. Gated dispatch

Every tool call in `pkg/exec` and every scheduled poll in the refresh loops consults the probe state before making any HTTP call to xolu:

- If `Healthy` is true (a pong exists within `MOLU_FRONT_PONG_FRESHNESS`), the call proceeds immediately.
- If `Healthy` is false, the call errors with `XOLU-MOLU-FRONT-UNAVAILABLE` and does not touch xolu.

Trusting the last pong within the freshness window is deliberate: it avoids the pathological case where every MCP tool call synchronously probes xolu before proceeding, which would multiply xolu's request rate. The freshness window is the window of time in which molu is willing to act on a stale pong.

### 8.3. Cadence and backoff

Under normal operation, the probe fires at `MOLU_FRONT_PING_INTERVAL` (default 30 seconds), with each request bounded by `MOLU_FRONT_PING_TIMEOUT` (default 5 seconds).

On a probe failure (timeout, non-2xx response, transport error), the probe enters a backoff loop:

- First retry after `MOLU_FRONT_PING_FAIL_FLOOR` (default 1 second).
- Each subsequent retry doubles the interval, up to `MOLU_FRONT_PING_FAIL_CEILING` (default 30 seconds).
- On the first successful pong after failure, cadence resets to the normal `MOLU_FRONT_PING_INTERVAL`.

During the backoff, `Healthy` remains false. All tool calls and scheduled polls continue to error until a pong succeeds.

### 8.4. Startup

At startup, molu waits for the first successful pong before opening the MCP transport for tool traffic. During this wait, molu logs at `info` level:

```
Waiting for xolu... attempt N/M (last error: <detail>)
```

Waiting is bounded by `MOLU_FRONT_STARTUP_MAX_ATTEMPTS` (default 60) at the normal `MOLU_FRONT_PING_INTERVAL` cadence — so with defaults, molu waits up to 30 minutes for xolu to come up before exiting with a non-zero status. Operators who want molu to exit sooner set the max lower; operators who want indefinite retry set it to `0` (interpreted as unlimited).

The MCP transport is *not* opened during the startup wait — an agent connecting to a molu whose xolu is not yet available would see a server whose tool list is empty (the semantic map has not been populated). It is more honest to keep the transport closed until molu can meaningfully serve.

### 8.5. Error mapping

When gated dispatch refuses a call, the error surfaces to the agent as an MCP tool error with structured detail:

```json
{
  "code":    "XOLU-MOLU-FRONT-UNAVAILABLE",
  "message": "xolu substrate is currently unreachable; retrying",
  "detail": {
    "last_pong_at":       "2026-07-17T14:32:11Z",
    "last_fail_at":       "2026-07-17T14:32:44Z",
    "consecutive_fails":  3,
    "next_retry_at":      "2026-07-17T14:32:52Z"
  }
}
```

The `XOLU-MOLU-FRONT-*` code prefix follows the xolu convention (`XOLU-<AREA><NUM>`) and is distinct from `XOLU-*` codes that originate inside xolu itself. Additional codes in this family:

- **`XOLU-MOLU-FRONT-UNAVAILABLE`** — xolu health probe is not currently green.
- **`XOLU-MOLU-FRONT-STARTUP`** — molu is still waiting for the first pong at startup.
- **`XOLU-MOLU-FRONT-TIMEOUT`** — a tool call reached xolu but exceeded `MOLU_FRONT_CALL_TIMEOUT`.
- **`XOLU-MOLU-FRONT-CONTRACT`** — molu-side validation of the call against the semantic map or a hub-published function's JSON Schema failed.
- **`XOLU-MOLU-FRONT-HUB-UNAVAILABLE`** — a domain-function call was requested but the hub-published function is no longer in the current catalogue.

Errors originating inside xolu (`XOLU-ST*`, `XOLU-QL*`, and so on) are surfaced to the agent with their original code preserved in the structured detail, so the agent can distinguish a xolu-level rejection from a molu-front-level one.

### 8.6. Configuration summary

Added to the configuration table in §10:

```
MOLU_FRONT_PING_INTERVAL            30s   probe cadence under normal operation
MOLU_FRONT_PING_TIMEOUT              5s   per-probe request timeout
MOLU_FRONT_PONG_FRESHNESS           45s   window during which last pong is trusted
MOLU_FRONT_PING_FAIL_FLOOR           1s   initial backoff after a failed probe
MOLU_FRONT_PING_FAIL_CEILING        30s   ceiling for exponential backoff
MOLU_FRONT_STARTUP_MAX_ATTEMPTS     60    max startup probe attempts (0 = unlimited)
```

---

## 9. Transport

molu supports two MCP transports, selected at startup by configuration:

- **stdio** (default): molu reads MCP messages from stdin and writes to stdout. Used when a local agent process (Claude Desktop, Cursor, an editor plugin) spawns molu as a child. Logging goes to stderr exclusively.
- **Streamable HTTP** (per MCP revision 2026-07-28): molu listens on an HTTP address, accepts MCP messages, and streams responses. Used when the agent runs elsewhere on the network.

SSE — the transport used in the initial MCP releases — is not supported. The current MCP spec has removed it in favour of Streamable HTTP.

Configuration:

```
MOLU_FRONT_TRANSPORT=stdio          # stdio | streamable-http
MOLU_FRONT_HTTP_ADDR=:8090          # only used for streamable-http
MOLU_FRONT_HTTP_AUTH=none           # none | bearer | mtls
MOLU_FRONT_HTTP_BEARER_TOKEN_FILE   # required when MOLU_FRONT_HTTP_AUTH=bearer
```

Authentication for the MCP transport itself is separate from authentication to xolu. On a shared network, molu **must** be configured with `bearer` or `mtls`; `none` is only appropriate for local stdio or loopback deployments.

---

## 10. Authentication to xolu

molu authenticates to xolu via the credential provided at configuration. It supports the three xolu auth modes:

- **Bearer token**: full authority, trusted-gateway pattern. Simplest configuration; least appropriate for multi-tenant deployments.
- **API key**: with `APIKeyGrants` scoping the tenants molu may act on.
- **JWT**: with signed `tenants: [...]` claim, or `tenant_admin: true` for admin authority.

Under `TenantAuthMode: scoped` on xolu, the credential carries the tenant boundary — molu does not negotiate tenant per-request. One molu instance, one tenant scope. Cross-tenant deployments run one molu per tenant.

Configuration:

```
MOLU_FRONT_XOLU_URL=http://localhost:8080
MOLU_FRONT_XOLU_AUTH_MODE=jwt        # bearer | apikey | jwt
MOLU_FRONT_XOLU_TOKEN_FILE=/etc/molu/xolu.jwt
```

Tokens are never read from command-line flags. Rotation is out-of-band: a SIGHUP triggers reload from the token file.

---

## 11. Configuration

The complete configuration surface, environment-variable form:

```
# Required
MOLU_FRONT_XOLU_URL                 xolu instance URL
MOLU_FRONT_XOLU_AUTH_MODE           bearer | apikey | jwt
MOLU_FRONT_XOLU_TOKEN_FILE          path to credential file

# Hub (optional)
MOLU_HUB_URL                  molu hub URL; if unset, no domain functions
MOLU_HUB_AUTH_MODE            same modes as xolu auth
MOLU_HUB_TOKEN_FILE

# MCP transport
MOLU_FRONT_TRANSPORT                stdio | streamable-http
MOLU_FRONT_HTTP_ADDR                :8090
MOLU_FRONT_HTTP_AUTH                none | bearer | mtls
MOLU_FRONT_HTTP_BEARER_TOKEN_FILE

# Refresh
MOLU_FRONT_SCHEMA_POLL_INTERVAL     60s
MOLU_FRONT_CATALOGUE_POLL_INTERVAL  60s

# xolu health probe
MOLU_FRONT_PING_INTERVAL            30s
MOLU_FRONT_PING_TIMEOUT             5s
MOLU_FRONT_PONG_FRESHNESS           45s
MOLU_FRONT_PING_FAIL_FLOOR          1s
MOLU_FRONT_PING_FAIL_CEILING        30s
MOLU_FRONT_STARTUP_MAX_ATTEMPTS     60         (0 = unlimited)

# Observability
MOLU_FRONT_LOG_LEVEL                info | debug | warn | error
MOLU_FRONT_LOG_FORMAT               console | json
MOLU_FRONT_METRICS_ADDR             :9090 (Prometheus /metrics; empty to disable)

# Behaviour
MOLU_FRONT_REDACT_PAYLOADS          true | false (default: true; payload contents never logged at info)
MOLU_FRONT_CALL_TIMEOUT             30s (default per-tool-call timeout)
```

All settings also accept a config file (`--config /path/to/molu.yaml`). Environment variables override file values. Command-line flags override both.

---

## 12. Availability behaviour

molu's behaviour under partial availability:

- **xolu reachable, hub reachable** — full operation. Generic primitives and domain functions available.
- **xolu reachable, hub unreachable** — degraded. Generic primitives only. The catalogue-refresh loop retains the last successful catalogue snapshot; if none exists (hub was never reached), no domain functions are registered. Warnings are logged at each failed refresh attempt.
- **xolu unreachable** — the health probe (§8) fails and all tool calls return `XOLU-MOLU-FRONT-UNAVAILABLE` with structured detail on the last successful pong and the next retry. The semantic map remains available for `describe` from the last successful poll, so the agent can at least see what xolu **would** offer if it were reachable. Existing MCP connections are not dropped; molu is transparent about degradation rather than opaque. molu itself does not attempt to reach xolu with tool traffic while the probe is red — this is deliberate: a struggling xolu should not have its load compounded by a molu that keeps trying.

xolu owns its own caching layer (`pkg/cache.Cache`, either in-process LRU or Redis) for entity, list, and query results. molu does not add a second cache. Every read goes through the xolu client and benefits from whatever cache configuration xolu is running.

If xolu is deployed in a fully disconnected environment, molu runs in that same environment. There is no cloud dependency to inherit and no external LLM call in the request path — molu itself performs no planning, so no LLM invocation is required for any tool call.

---

## 13. Future work

Recorded so a reader knows what is *not* in draft-02 and where it will land.

**Event subscriptions for proactive MCP notifications.** molu does not subscribe to the xolu event stream in draft-02. A future revision may register subscriptions to `entity.updated`, `fsm.step`, and `commit.applied` for the purpose of pushing MCP notifications to the agent — allowing an agent to be told about state changes without polling. When that work happens, subscriptions will use a namespaced description prefix (candidate: `molu.front.<instance-id>.*`) for identification and cleanup.

**cal `move` and lifecycle-completion tools.** `cal_check`, `cal_openings`, `cal_propose`, and `cal_confirm` cover the primary agentic scheduling flow in draft-02. A future revision will add `cal_move` (atomic reschedule) and `cal_complete` / `cal_cancel` (terminal lifecycle transitions) once the request patterns from real agentic use are observed.

**Sync via nolu.** When nolu-coordinated xolu deployments become the norm, molu's semantic-map poller may need awareness of the coordination layer to avoid transient inconsistencies during nolu rebalancing. Not in draft-02; the current design assumes one molu talks to one xolu directly.

---

*Draft 02. Pre-implementation. Assumes xolu ≥ v0.15.0.*
