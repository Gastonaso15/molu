# Plan Técnico: Spec 007 — Telemetry & Observability

## Archivos y Responsabilidades

| Archivo | Responsabilidad |
|---------|-----------------|
| `pkg/telemetry/types.go` | Definición de structs compartidos: `LogEntry`, `LogDomain`, `LogLevel`, `RequestContext`, `RetryMetadata` (reutiliza de taxonomy). |
| `pkg/telemetry/logger.go` | Interfaz `Logger` con métodos por dominio: `System()`, `Application()`, `Security()`, `Network()`. Implementación `zerologLogger`. |
| `pkg/telemetry/logger_test.go` | Tests unitarios de constructores, niveles, formato JSON/console, campos base obligatorios. |
| `pkg/telemetry/rotation.go` | `LogRotator` con rotación por tamaño/tiempo, compresión gzip, escritura atómica. |
| `pkg/telemetry/rotation_test.go` | Tests de rotación, retención, concurrencia, no pérdida. |
| `pkg/telemetry/metrics.go` | Interfaz `Metrics` + implementación Prometheus: contadores peticiones, histogramas latencia, contadores errores por código taxonomía. |
| `pkg/telemetry/metrics_test.go` | Tests de registro y recolección de métricas. |
| `pkg/telemetry/health.go` | Interfaz `Health` + implementación: `/healthz` (liveness/readiness), verifica `XoluClient.Ping()` cacheado 30s. |
| `pkg/telemetry/health_test.go` | Tests de health check estados OK/Degraded/Down. |
| `pkg/telemetry/middleware.go` | Middleware MCP: genera `request_id` (UUID v7), extrae/propaga `trace_id` (W3C traceparent), inyecta logger en contexto. |
| `pkg/telemetry/middleware_test.go` | Tests de correlación IDs, propagación headers salientes a xolu. |
| `pkg/telemetry/config.go` | Estructuras de configuración: `LogConfig`, `TelemetryConfig`, `SecurityLogConfig`; parsing variables `MOLU_FRONT_*`. |
| `pkg/telemetry/config_test.go` | Tests de defaults, parsing, validación. |
| `pkg/xolu/client.go` | Añadir campos `Logger`, `Metrics` a `XoluClient`; loggear invocaciones `Walk`/`Find`/`Get` con latencia y código taxonomía. |
| `pkg/rpi/admit_score.go` | Añadir `Logger` a `RPIProvider`; loggear decisiones `Admit`/`Score` en dominio aplicación/seguridad. |
| `cmd/molu/main.go` | Wiring: inicializar `Logger`, `Metrics`, `Health`, `LogRotator`; registrar middleware MCP; arrancar servidor telemetry en puerto separado. |
| `docs/deployment/007-telemetry-deployment.md` | Despliegue: orden tolerado, puertos (MCP vs telemetry), reintentos backoff, transportes stdio/streamable-http. |
| `docs/configuration/007-telemetry-config.md` | Configuración: lista exhaustiva `MOLU_FRONT_LOG_*`, `MOLU_FRONT_TELEMETRY_*`, `MOLU_FRONT_SECURITY_LOG_*` con defaults y justificación. |
| `docs/runbook/007-telemetry-runbook.md` | Runbook: diagnóstico por dominio, correlación `request_id`, códigos taxonomía en logs, procedimiento `SUBSTRATE_UNAVAILABLE`. |
| `docs/diagrams/007-telemetry-sequence.mmd` | Diagrama de secuencia Mermaid: petición MCP → logs 4 dominios + métricas. |
| `docs/diagrams/007-telemetry-error-sequence.mmd` | Diagrama: error taxonomía → correlación `request_id` en `details` + log seguridad/red. |
| `docs/diagrams/007-telemetry-health-metrics.mmd` | Diagrama: health check + métricas Prometheus. |

## Modelos y Contratos

```go
// pkg/telemetry/types.go
type LogDomain string

const (
    DomainSystem     LogDomain = "system"
    DomainApplication LogDomain = "application"
    DomainSecurity   LogDomain = "security"
    DomainNetwork    LogDomain = "network"
)

type LogLevel string

const (
    LevelDebug LogLevel = "debug"
    LevelInfo  LogLevel = "info"
    LevelWarn  LogLevel = "warn"
    LevelError LogLevel = "error"
    LevelFatal LogLevel = "fatal"
)

type LogEntry struct {
    Timestamp       string         `json:"timestamp"`
    Level           string         `json:"level"`
    Domain          string         `json:"domain"`
    Component       string         `json:"component"`
    RequestID       string         `json:"request_id,omitempty"`
    TraceID         string         `json:"trace_id,omitempty"`
    SpanID          string         `json:"span_id,omitempty"`
    Message         string         `json:"message"`
    Fields          map[string]any `json:"fields,omitempty"`
    // Seguridad-specific
    Actor           string         `json:"actor,omitempty"`
    Action          string         `json:"action,omitempty"`
    Resource        string         `json:"resource,omitempty"`
    Decision        string         `json:"decision,omitempty"` // ALLOW/DENY
    Reason          string         `json:"reason,omitempty"`
}

// RequestContext propagado via context.Context
type RequestContext struct {
    RequestID string
    TraceID   string
    SpanID    string
    TenantID  string
    UserID    string
    Method    string
    Tool      string
}

type Logger interface {
    System()     *zerolog.Event
    Application() *zerolog.Event
    Security()   *zerolog.Event
    Network()    *zerolog.Event
    WithContext(ctx context.Context) Logger
    WithFields(fields map[string]any) Logger
    Level() LogLevel
    SetLevel(level LogLevel)
}

type Metrics interface {
    IncRequests(component, method, tool, code string)
    ObserveLatency(component, method string, durationMs float64)
    IncErrors(component, code string)
    Handler() http.Handler // /metrics endpoint
}

type Health interface {
    Handler() http.Handler // /healthz endpoint
    Ready() bool
    Live() bool
    SetXoluStatus(available bool, lastPing time.Time)
}
```

```go
// pkg/telemetry/config.go
type LogConfig struct {
    Format           string `env:"MOLU_FRONT_LOG_FORMAT" default:"json"`           // json | console
    Level            string `env:"MOLU_FRONT_LOG_LEVEL" default:"info"`            // debug | info | warn | error
    MaxSizeMB        int    `env:"MOLU_FRONT_LOG_MAX_SIZE_MB" default:"100"`
    MaxAgeDays       int    `env:"MOLU_FRONT_LOG_MAX_AGE_DAYS" default:"7"`
    Compress         bool   `env:"MOLU_FRONT_LOG_COMPRESS" default:"true"`
    OutputPath       string `env:"MOLU_FRONT_LOG_OUTPUT_PATH" default:"logs"`      // directorio
    SecurityMaxAgeDays int  `env:"MOLU_FRONT_SECURITY_LOG_MAX_AGE_DAYS" default:"90"`
}

type TelemetryConfig struct {
    Port         int    `env:"MOLU_FRONT_TELEMETRY_PORT" default:"9090"`
    PathMetrics  string `env:"MOLU_FRONT_TELEMETRY_METRICS_PATH" default:"/metrics"`
    PathHealth   string `env:"MOLU_FRONT_TELEMETRY_HEALTH_PATH" default:"/healthz"`
    Enabled      bool   `env:"MOLU_FRONT_TELEMETRY_ENABLED" default:"true"`
}
```

## Mapeo de Requisitos a Implementación

| RF | Cobertura |
|----|-----------|
| RF-1 (startup log) | `cmd/molu/main.go` → `logger.System().Info().Msg("startup")` con versión, flags, PID |
| RF-2 (request_id, trace_id) | `middleware.go` → genera UUID v7, extrae traceparent, inyecta en contexto |
| RF-3 (RPI Admit/Score) | `pkg/rpi/admit_score.go` → logger.Application().Debug() / logger.Security().Warn() |
| RF-4 (invocation log) | `pkg/xolu/client.go` → logger.Application().Info() inicio/fin con latencia, código taxonomía |
| RF-5 (xolu error) | `pkg/xolu/client.go` → logger.Network().Error() con RetryMetadata |
| RF-6 (schema validation) | `cmd/molu/main.go` (validación) → logger.Application().Warn() con fields |
| RF-7 (auth decision) | `pkg/rpi/admit_score.go` → logger.Security().Info() con actor/action/resource/decision/reason |
| RF-8 (shutdown log) | `cmd/molu/main.go` (signal handler) → logger.System().Info() uptime, contadores |
| RF-9 (format/level config) | `config.go` + `logger.go` → `MOLU_FRONT_LOG_FORMAT`, `MOLU_FRONT_LOG_LEVEL` |
| RF-10 (rotation) | `rotation.go` → `LogRotator` con lumberjack o implementación propia |
| RF-11 (health/metrics) | `health.go` + `metrics.go` → servidor HTTP en puerto `MOLU_FRONT_TELEMETRY_PORT` |
| RF-12 (security log separate) | `logger.go` → `Security()` escribe a `security.log` con `sync.Write` |
| RF-13 (propagate headers) | `middleware.go` + `pkg/xolu/client.go` → `X-Request-ID`, `X-Trace-ID` |
| RF-14 (zerolog) | `logger.go` → implementación con `github.com/rs/zerolog` |

## Concurrencia y Resiliencia

- `LogRotator`: `sync.Mutex` para rotación atómica; writers concurrentes seguros.
- `Logger`: `zerolog` es thread-safe; `WithContext` retorna nuevo logger con campos enriquecidos (inmutable).
- `Metrics`: `prometheus/client_golang` usa atomic counters; seguro para concurrencia.
- `Health`: `sync.RWMutex` para estado xolu cacheado (TTL 30s).
- `RequestContext`: propagado via `context.Context` (value type) — inmutable, seguro.
- Cero data races: verificado con `go test -v -race ./...`.

## Decisiones Técnicas y Alternativas Descartadas

| Decisión | Justificación | Alternativa Descartada |
|----------|---------------|------------------------|
| `zerolog` sobre `zap` | API fluida, zero-allocation JSON, más ligero, mejor performance en benchmarks. | `zap` — más verboso, allocations en campos, curva aprendizaje mayor. |
| UUID v7 para `request_id` | Timestamp embebido → ordenable, correlacionable sin clock sync. | UUID v4 — aleatorio, no ordenable; ULID — dependencia extra. |
| Security log síncrono (flush inmediato) | Integridad auditoría: cada entrada persistida antes de continuar. | Buffered/async — riesgo pérdida en crash; inaceptable para auditoría. |
| Puerto telemetry separado (9090) | Desacopla salud/métricas del plano de control MCP; evita contención. | Mismo puerto — mezcla preocupaciones, expone métricas a clientes MCP. |
| `LogRotator` propia (no lumberjack) | Control total: rotación por tamaño+tiempo, compresión, security log separado. | `lumberjack` — no soporta security log separado ni flush síncrono configurable. |
| W3C traceparent para `trace_id` | Estándar industria; interoperable con proxies, load balancers, APM. | Propietario — requiere instrumentación custom en toda la cadena. |

## Estrategia de Tests

### Unitarios (`go test -v -race ./pkg/telemetry/...`)
- `TestLogger_JSONFormat`: salida JSON tiene campos base obligatorios.
- `TestLogger_ConsoleFormat`: salida consola legible, colores.
- `TestLogger_LevelFilter`: nivel config filtra correctamente.
- `TestLogger_Domains`: cada dominio escribe a writer correcto.
- `TestLogger_SecuritySync`: security log flush inmediato (verificar `fsync`).
- `TestLogRotator_Rotation`: rotación por tamaño y tiempo, compresión.
- `TestLogRotator_Retention`: archivos viejos eliminados según `MaxAgeDays`.
- `TestLogRotator_Concurrency`: escrituras paralelas sin corrupción.
- `TestMetrics_Counters`: contadores peticiones, errores por código taxonomía.
- `TestMetrics_LatencyHistogram`: histogramas latencia por componente/método.
- `TestHealth_ReadyLive`: estados OK/Degraded/Down según xolu ping.
- `TestMiddleware_RequestID`: UUID v7 generado, formato correcto.
- `TestMiddleware_TraceParent`: extrae W3C traceparent, genera si ausente.
- `TestMiddleware_Propagation`: headers `X-Request-ID`, `X-Trace-ID` en llamada xolu.
- `TestConfig_Defaults`: valores por defecto correctos.
- `TestConfig_Parsing`: env vars parsean correctamente.

### Integración (`go test -v -race ./pkg/telemetry/... -run Integration`)
- **TestTelemetry_FullRequestFlow**: petición MCP → logs en 4 dominios correlacionados + métricas incrementadas.
- **TestTelemetry_TaxonomyErrorCorrelation**: error `NOT_FOUND` → `request_id` en `details` + log network/error + métrica error.
- **TestTelemetry_SecurityAuthDecision**: `Admit` retorna vacío → log security WARN con `NOT_OFFERED` + decisión DENY.
- **TestTelemetry_XoluUnavailable**: `XoluClient.Ping` falla → log network ERROR con `SUBSTRATE_UNAVAILABLE` + health 503.
- **TestTelemetry_Shutdown**: SIGTERM → log system INFO con uptime/contadores.
- **TestTelemetry_RotationUnderLoad**: rotación durante carga concurrente → sin pérdida, archivos íntegros.

### Concurrencia
- `go test -v -race ./...` en todo el repo (CI gate).
- Tests paralelos (`t.Parallel()`) en loggers, rotator, metrics.

### Herramientas
- `testify/require` para aserciones.
- `go.uber.org/mock` o mocks manuales en `*_mock.go`.
- `mmdc` para generar `.pdf` desde `.mmd` en CI.
- `github.com/google/uuid` para UUID v7 (o `github.com/oklog/ulid` si v7 no disponible).

## Matriz de Cobertura de Requisitos

| RF | Test Unitario | Test Integración |
|----|---------------|------------------|
| RF-1 | `TestLogger_Startup` | `TestTelemetry_StartupLogs` |
| RF-2 | `TestMiddleware_RequestID`, `TestMiddleware_TraceParent` | `TestTelemetry_FullRequestFlow` |
| RF-3 | `TestRPI_AdmitScoreLogging` | `TestTelemetry_SecurityAuthDecision` |
| RF-4 | `TestXoluClient_InvocationLogging` | `TestTelemetry_FullRequestFlow` |
| RF-5 | `TestXoluClient_ErrorLogging` | `TestTelemetry_XoluUnavailable` |
| RF-6 | `TestValidation_Logging` | `TestTelemetry_ContractViolation` |
| RF-7 | `TestRPI_AuthDecisionLogging` | `TestTelemetry_SecurityAuthDecision` |
| RF-8 | `TestLogger_Shutdown` | `TestTelemetry_Shutdown` |
| RF-9 | `TestLogger_JSONFormat`, `TestLogger_ConsoleFormat`, `TestLogger_LevelFilter` | — |
| RF-10 | `TestLogRotator_Rotation`, `TestLogRotator_Retention` | `TestTelemetry_RotationUnderLoad` |
| RF-11 | `TestHealth_ReadyLive`, `TestMetrics_Handler` | `TestTelemetry_HealthMetricsEndpoints` |
| RF-12 | `TestLogger_SecuritySync` | `TestTelemetry_SecurityLogSeparateFile` |
| RF-13 | `TestMiddleware_Propagation` | `TestTelemetry_FullRequestFlow` |
| RF-14 | `TestLogger_ZeroAlloc` (benchmark) | — |