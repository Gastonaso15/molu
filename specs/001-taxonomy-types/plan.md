# Plan Técnico: Spec 001 — Taxonomy Types

## Archivos y Responsabilidades

| Archivo | Responsabilidad |
|---------|-----------------|
| `pkg/taxonomy/types.go` | Definición de structs compartidos: `TypedError`, `ErrorCode`, `RetryMetadata`. Usado por molu y Hub. |
| `pkg/taxonomy/errors.go` | Constructores para cada código tipado con prefijo `XOLU-MOLU-FRONT-*` y preservación verbatim de `XOLU-*`. |
| `pkg/taxonomy/errors_test.go` | Tests unitarios de constructores, serialización JSON, prefijos y verbatim. |
| `pkg/taxonomy/integration_test.go` | Tests de integración dedicados por cada uno de los 6 códigos + preservación verbatim (7 tests). |
| `pkg/xolu/client.go` | Interfaz `XoluClient` desacoplada (mockable): `Walk`, `Find`, `Get`, `Ping`. |
| `pkg/xolu/client_mock.go` | Mock para tests (implementa `XoluClient` con comportamiento controlable). |
| `pkg/mcp/transport.go` | Interfaz `MCPTransport` desacoplada (mockable): `Send`, `Receive`, `Close`. |
| `pkg/mcp/transport_mock.go` | Mock para tests de transporte. |
| `pkg/rpi/admit_score.go` | Interfaz `RPIProvider` con `Admit(ctx, query, candidates)` y `Score(ctx, query, admitted)`. Proveedores V1: `admit-all`, `static-context-rules`. |
| `cmd/molu/main.go` | Punto de entrada: wiring de dependencias, flags `MOLU_FRONT_*`, registro de herramientas MCP. |
| `docs/diagrams/001-taxonomy-sequence.mmd` | Diagrama de secuencia Mermaid (fuente). |
| `docs/diagrams/001-taxonomy-sequence.pdf` | Diagrama renderizado (generado con `mmdc`). |

## Modelos y Contratos

```go
// pkg/taxonomy/types.go
type ErrorCode string

const (
    CodeNotOffered          ErrorCode = "NOT_OFFERED"
    CodeNotFound            ErrorCode = "NOT_FOUND"
    CodeEmpty               ErrorCode = "EMPTY"
    CodeInvalidTransition   ErrorCode = "INVALID_TRANSITION"
    CodeContractViolation   ErrorCode = "CONTRACT_VIOLATION"
    CodeSubstrateUnavailable ErrorCode = "SUBSTRATE_UNAVAILABLE"
)

type TypedError struct {
    Code    string         `json:"code"`
    Message string         `json:"message"`
    Details map[string]any `json:"details,omitempty"`
}

type RetryMetadata struct {
    LastSuccessfulContact string `json:"lastSuccessfulContact"` // RFC3339
    NextRetryAt           string `json:"nextRetryAt"`           // RFC3339
    AttemptNumber         int    `json:"attemptNumber"`
    MaxAttempts           int    `json:"maxAttempts"`
}

// Prefijos
const (
    PrefixMolu = "XOLU-MOLU-FRONT-"
    PrefixXolu = "XOLU-"
)
```

```go
// pkg/xolu/client.go
type XoluClient interface {
    Walk(ctx context.Context, req WalkRequest) (WalkResponse, error)
    Find(ctx context.Context, req FindRequest) (FindResponse, error)
    Get(ctx context.Context, req GetRequest) (GetResponse, error)
    Ping(ctx context.Context) error
}
```

```go
// pkg/rpi/admit_score.go
type RPIProvider interface {
    Admit(ctx context.Context, query string, candidates []ToolCandidate) ([]ToolCandidate, error)
    Score(ctx context.Context, query string, admitted []ToolCandidate) ([]ScoredTool, error)
}

type ToolCandidate struct {
    Name        string
    Namespace   string // vacío para primitivos genéricos
    Description string
    InputSchema map[string]any
}

type ScoredTool struct {
    ToolCandidate
    Score float64
}
```

## Mapeo Explícito a Taxonomía Tipada (§2.4)

| Código Taxonomía | Prefijo Propio | Origen | Preservación Verbatim |
|------------------|----------------|--------|----------------------|
| NOT_OFFERED | `XOLU-MOLU-FRONT-NOT_OFFERED` | molu/Hub (RPI Admit) | No aplica |
| NOT_FOUND | `XOLU-MOLU-FRONT-NOT_FOUND` | molu/Hub o xolu | `XOLU-NOT_FOUND` de xolu |
| EMPTY | `XOLU-MOLU-FRONT-EMPTY` | molu/Hub (consulta válida sin resultados) | No aplica |
| INVALID_TRANSITION | `XOLU-MOLU-FRONT-INVALID_TRANSITION` | molu (wrap) | `XOLU-INVALID_TRANSITION` + mensaje guarda verbatim |
| CONTRACT_VIOLATION | `XOLU-MOLU-FRONT-CONTRACT_VIOLATION` | molu (validación schema) | No aplica |
| SUBSTRATE_UNAVAILABLE | `XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE` | molu (xolu inalcanzable) | `XOLU-*` genérico de xolu si existe |

**Regla**: Cualquier `XOLU-*` no mapeado arriba se propaga verbatim (RF-7).

## Interfaz RPI (Admit/Score) — Eje 1

- `Admit` filtra candidatos por tenant/rol → puede retornar `NOT_OFFERED` si lista admitida queda vacía.
- `Score` ordena admitidos por relevancia → no emite códigos de taxonomía.
- Proveedores V1 implementados en `pkg/rpi/providers/`:
  - `admit-all`: admite todo, score fijo 1.0.
  - `static-context-rules`: reglas YAML/JSON por tenant/rol (fuera de alcance en esta spec, solo interfaz).

## Diagrama de Secuencia (Mermaid)

```mermaid
sequenceDiagram
    participant Agent as Agente MCP
    participant Molu as molu (Front MCP)
    participant Hub as Hub Registry
    participant RPI as RPI Provider
    participant Xolu as xolu Sustrato

    Agent->>Molu: tools/call {name: "walk", args: {...}}
    Molu->>Hub: ResolveTool("walk")
    Hub->>RPI: Admit(ctx, query, candidates)
    RPI-->>Hub: admitted[] (puede ser vacío → NOT_OFFERED)
    Hub-->>Molu: ToolDescriptor admitido
    
    alt Tool es primitivo genérico (walk/find/get)
        Molu->>Xolu: Walk/Find/Get via XoluClient
        alt Xolu responde OK
            Xolu-->>Molu: Resultado
            Molu-->>Agent: Respuesta exitosa
        else Xolu rechaza transición FSM
            Xolu-->>Molu: error {code: "XOLU-INVALID_TRANSITION", message: "guarda: estado inválido"}
            Molu->>Molu: Preservar verbatim
            Molu-->>Agent: error {code: "XOLU-INVALID_TRANSITION", message: "guarda: estado inválido"}
        else Xolu inalcanzable (timeout/5xx)
            Molu->>Molu: Construir SUBSTRATE_UNAVAILABLE con RetryMetadata
            Molu-->>Agent: error {code: "XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE", details: {lastSuccessfulContact, nextRetryAt, attemptNumber, maxAttempts}}
        end
    else Tool es función de dominio (namespace.Nombre)
        Molu->>Xolu: Walk (ejecuta FSM de dominio)
        ... mismo flujo ...
    end
```

## Concurrencia y Resiliencia

- `context.Context` propagado en toda la cadena (timeouts, cancelación).
- `XoluClient.Ping` con backoff exponencial: `MOLU_FRONT_PING_FAIL_FLOOR` (ej. 1s) → `MOLU_FRONT_PING_FAIL_CEILING` (ej. 30s) hasta `MOLU_FRONT_STARTUP_MAX_ATTEMPTS` (ej. 10).
- `sync.RWMutex` en catálogo Hub para lecturas concurrentes de herramientas admitidas.
- Cero data races: verificado con `go test -v -race ./...`.

## Decisiones Técnicas y Alternativas Descartadas

| Decisión | Justificación | Alternativa Descartada |
|----------|---------------|------------------------|
| Paquete `pkg/taxonomy/` interno (no módulo separado) | Simplicidad operativa; molu y Hub comparten repo; versionado atómico. | Módulo Go separado `github.com/molu/taxonomy` — añade complejidad de publicación y versionado sin beneficio inmediato. |
| Formato `XOLU-<CODE>` fijo para códigos de xolu | Contrato estable; evita parsing heurístico; alinea con nuestra taxonomía. | Formato libre con mapeo por prefijo — frágil, propenso a colisiones, difícil de testear. |
| `RetryMetadata` con `attemptNumber`/`maxAttempts` | Visibilidad operativa completa; runbook puede alertar en intento N de M. | Solo `lastSuccessfulContact`/`nextRetryAt` — insuficiente para diagnosticar backoff agotado. |
| Interfaces `XoluClient` y `MCPTransport` en paquetes separados | Desacoplamiento total; mocks independientes por capa; testabilidad. | Interfaces en mismo paquete que implementación — acopla tests a detalles internos. |
| `RPIProvider` como interfaz (no struct) | Permite múltiples implementaciones (admit-all, static-rules, OPA, etc.) sin cambiar molu/Hub. | Lógica de admit/score embebida en Hub — viola Eje 1, impide swap de proveedores. |
| Constructores de error en `pkg/taxonomy/errors.go` (no métodos en `TypedError`) | Funciones puras, sin estado, fáciles de testear y mockear. | Métodos en struct — requiere instanciar, menos idiomático para errores. |

## Matriz de Cobertura de Requisitos

| RF | Cobertura |
|----|-----------|
| RF-1 (NOT_OFFERED) | `pkg/taxonomy/errors.go:NewNotOffered`, test `TestTaxonomy_NotOffered` |
| RF-2 (NOT_FOUND) | `pkg/taxonomy/errors.go:NewNotFound` + `WrapXoluNotFound`, test `TestTaxonomy_NotFound` + `TestTaxonomy_XoluNotFoundVerbatim` |
| RF-3 (EMPTY) | `pkg/taxonomy/errors.go:NewEmpty`, test `TestTaxonomy_Empty` |
| RF-4 (INVALID_TRANSITION) | `pkg/taxonomy/errors.go:WrapXoluInvalidTransition`, test `TestTaxonomy_InvalidTransitionVerbatim` |
| RF-5 (CONTRACT_VIOLATION) | `pkg/taxonomy/errors.go:NewContractViolation`, test `TestTaxonomy_ContractViolation` |
| RF-6 (SUBSTRATE_UNAVAILABLE) | `pkg/taxonomy/errors.go:NewSubstrateUnavailable`, test `TestTaxonomy_SubstrateUnavailable` |
| RF-7 (Verbatim otros XOLU-*) | `pkg/taxonomy/errors.go:WrapXoluError`, test `TestTaxonomy_XoluVerbatimPassthrough` |
| RF-8 (No error genérico) | Tests de integración validan que **nunca** se retorna `error` genérico sin código tipado. |

## Estrategia de Tests

### Unitarios (`go test -v -race ./pkg/taxonomy/... ./pkg/rpi/...`)
- `TestNewNotOffered`, `TestNewNotFound`, `TestNewEmpty`, `TestNewContractViolation`, `TestNewSubstrateUnavailable`
- `TestWrapXoluNotFound`, `TestWrapXoluInvalidTransition`, `TestWrapXoluVerbatimPassthrough`
- Serialización JSON: `code`, `message`, `details` correctos.
- Prefijos: `XOLU-MOLU-FRONT-*` en propios; `XOLU-*` sin modificar en verbatim.
- `RPIProvider` mocks: `admit-all` y `static-context-rules` (stub).

### Integración (`go test -v -race ./pkg/taxonomy/... -run Integration`)
- **TestTaxonomy_NotFound**: Llamada `Find` a objeto inexistente → `XOLU-MOLU-FRONT-NOT_FOUND`.
- **TestTaxonomy_XoluNotFoundVerbatim**: `XoluClient` mock devuelve `XOLU-NOT_FOUND` → propagado verbatim.
- **TestTaxonomy_Empty**: `Find` con query válida sin resultados → `XOLU-MOLU-FRONT-EMPTY`.
- **TestTaxonomy_InvalidTransitionVerbatim**: `Walk` rechaza transición → `XOLU-INVALID_TRANSITION` con mensaje guarda exacto.
- **TestTaxonomy_ContractViolation**: Input inválido vs schema → `XOLU-MOLU-FRONT-CONTRACT_VIOLATION` con `details.fields`.
- **TestTaxonomy_SubstrateUnavailable**: `XoluClient.Ping` falla tras `MAX_ATTEMPTS` → `XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE` con `RetryMetadata` completo.
- **TestTaxonomy_XoluVerbatimPassthrough**: `XoluClient` devuelve `XOLU-CUSTOM_CODE` → propagado sin cambio.

### Concurrencia
- `go test -v -race ./...` en todo el repo (CI gate).
- Tests paralelos (`t.Parallel()`) en constructores y mocks.

### Herramientas
- `testify/require` para aserciones.
- `go.uber.org/mock` o mocks manuales en `*_mock.go` (preferido: manuales, sin dependencias extra).
- `mmdc` (Mermaid CLI) para generar `.pdf` desde `.mmd` en CI.