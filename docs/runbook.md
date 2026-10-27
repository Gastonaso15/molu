# Runbook Operativo: Telemetría y Observabilidad (Spec 007)

## Propósito

Guía operativa para diagnosticar y resolver incidencias usando el subsistema de telemetría de **molu** (logging 4 dominios, métricas Prometheus, health checks, correlación de requests).

---

## Arquitectura de Observabilidad

```
┌────────────────────────────────────────────────────────────────────────┐
│                         MOLU FRONTAL MCP                                │
├────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐             │
│  │   Logger     │    │   Metrics    │    │   Health     │             │
│  │  (4 dominios)│    │  (Prometheus)│    │  (HTTP 9090) │             │
│  └──────┬───────┘    └──────┬───────┘    └──────┬───────┘             │
│         │                   │                   │                      │
│         ▼                   ▼                   ▼                      │
│  ┌──────────────────────────────────────────────────────────────┐    │
│  │                    Correlación (request_id, trace_id)         │    │
│  └──────────────────────────────────────────────────────────────┘    │
│                                                                         │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 1. Diagnóstico por Dominio de Log

### 1.1 Sistema (`system.log`)

| Evento | Nivel | Acción |
|--------|-------|--------|
| Arranque | INFO | Verificar versión, flags, PID |
| Apagado | INFO | Ver uptime, contadores requests |
| Config inválida | ERROR | Revisar variables de entorno |
| SIGHUP reload | INFO | Verificar schemas recargados |

**Query Loki/Elastic**: `{domain="system"}`

### 1.2 Aplicación (`application.log`)

| Evento | Nivel | Acción |
|--------|-------|--------|
| Tool call start | INFO | Normal (latencia esperada < 100ms) |
| Tool call end | INFO | Verificar `code` (OK/NOT_FOUND/...) y `latency_ms` |
| Validation error | WARN | Revisar `code=CONTRACT_VIOLATION`, `details.fields` |
| RPI Admit/Score | DEBUG | Solo si `LOG_LEVEL=debug` |

**Query**: `{domain="application"} | json | latency_ms > 500` → detectar latencia alta

### 1.3 Seguridad (`security.log`)

| Evento | Nivel | Acción |
|--------|-------|--------|
| Admit vacío (DENY) | WARN | Revisar `reason=not_offered`, `resource`, `actor` |
| Auth decision ALLOW | INFO | Auditoria normal |
| Auth decision DENY | WARN | Investigar `reason` (not_offered, unauthorized, forbidden) |
| Config sensible | WARN | Cambio de config sensible detectado |

**Query**: `{domain="security"} | json | decision="DENY"` → alertar en tiempo real

**Importante**: `security.log` tiene **flush síncrono** — cada línea está en disco al retornar.

### 1.4 Red/Acceso (`network.log`)

| Evento | Nivel | Acción |
|--------|-------|--------|
| MCP request | INFO | Correlacionar con `request_id` |
| xolu ping | INFO | Ver `latency_ms` (esperado < 50ms) |
| xolu call | INFO/ERROR | Ver `code` y `latency_ms` |
| xolu unreachable | ERROR | `code=SUBSTRATE_UNAVAILABLE` → alertar |

**Query**: `{domain="network", level="error"}` → alertar inmediatamente

---

## 2. Correlación de Requests (`request_id`, `trace_id`)

### Formato de IDs

| ID | Formato | Generación | Propagación |
|----|---------|------------|-------------|
| `request_id` | ULID (26 chars, Crockford Base32) | ULID v2 (time-ordered) | Header `X-Request-ID` |
| `trace_id` | 32 hex chars (W3C) | ULID o header `traceparent` | Header `X-Trace-ID`, `traceparent` |
| `span_id` | 16 hex chars | ULID o header `traceparent` | Header `traceparent` |

### Flujo de Correlación

```
Cliente MCP → [Middleware] → request_id (ULID) + trace_id (W3C)
                    │
                    ▼
            Context enriquecido
                    │
                    ▼
         Logger.WithContext(ctx)
                    │
                    ▼
         xolu client (headers X-Request-ID, X-Trace-ID)
                    │
                    ▼
            Logs 4 dominios + métricas
```

### Búsqueda por Correlación

```bash
# Por request_id (más preciso)
grep "01M50Z7PBGKTWJG65S2PAGVYPN" /var/log/molu/*.log

# Por trace_id (traza distribuida)
grep "0af7651916cd43dd8448eb211c80319c" /var/log/molu/*.log

# En Loki/Elastic
{request_id="01M50Z7PBGKTWJG65S2PAGVYPN"}
{trace_id="0af7651916cd43dd8448eb211c80319c"}
```

---

## 3. Códigos de Taxonomía en Logs

Cada error tipado incluye `code` y `details`:

| Código | Significado | Dominio típico | Detalles clave |
|--------|-------------|----------------|----------------|
| `XOLU-MOLU-FRONT-NOT_OFFERED` | Función fuera de alcance | security | `tenant`, `tool` |
| `XOLU-MOLU-FRONT-NOT_FOUND` | Objeto inexistente | network/application | `tenant`, `objectId` |
| `XOLU-MOLU-FRONT-EMPTY` | Consulta sin resultados | application | `tenant`, `operation`, `query.*` |
| `XOLU-MOLU-FRONT-INVALID_TRANSITION` | FSM rechazada por xolu | network | `message` = guarda verbatim |
| `XOLU-MOLU-FRONT-CONTRACT_VIOLATION` | Schema inválido | application | `fields[]` infractores |
| `XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE` | xolu caído | network | `lastSuccessfulContact`, `nextRetryAt`, `attemptNumber`, `maxAttempts` |
| `XOLU-*` (verbatim) | Otros códigos xolu | network | Preservado sin modificación |

**Regla**: Nunca colapsar a "internal error". Cada código tiene test de integración dedicado.

---

## 4. Procedimiento: `SUBSTRATE_UNAVAILABLE`

### Síntomas
- Log `network` nivel ERROR: `code=XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE`
- Health check `/healthz` → 503, status `degraded`
- Métricas: `molu_errors_total{code="SUBSTRATE_UNAVAILABLE"}` incrementa
- Requests MCP fallan con 503 o error taxonomía

### Detalles en Log
```json
{
  "code": "XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE",
  "details": {
    "lastSuccessfulContact": "2026-10-15T14:30:00Z",
    "nextRetryAt": "2026-10-15T14:30:30Z",
    "attemptNumber": 3,
    "maxAttempts": 10
  }
}
```

### Pasos de Resolución

| Paso | Acción | Verificación |
|------|--------|--------------|
| 1 | Verificar conectividad a `MOLU_FRONT_XOLU_URL` | `curl -f http://xolu:8080/health` |
| 2 | Revisar logs de xolu | `docker logs xolu` / `journalctl -u xolu` |
| 3 | Verificar red/firewall | `nc -zv xolu 8080` |
| 4 | Revisar recursos xolu (CPU, RAM, disco) | `docker stats xolu` |
| 5 | Si xolu caído: reiniciar xolu | `systemctl restart xolu` / `docker restart xolu` |
| 6 | Verificar auto-recuperación | `curl http://molu:9090/healthz` → 200 OK |

### Auto-recuperación
- molu reintenta ping con backoff exponencial (1s → 30s)
- Health check usa cache 30s: `/healthz` sigue 503 hasta 30s tras recuperación
- Métrica `molu_errors_total{code="SUBSTRATE_UNAVAILABLE"}` deja de incrementar

---

## 5. Métricas Prometheus Clave

| Métrica | Tipo | Labels | Uso |
|---------|------|--------|-----|
| `molu_requests_total` | Counter | `component`, `method`, `tool`, `code` | Throughput, tasa de error por tool |
| `molu_latency_seconds` | Histogram | `component`, `method` | Latencia p50/p95/p99 por operación |
| `molu_errors_total` | Counter | `component`, `code` | Conteo errores por taxonomía |

### Alertas Recomendadas (PrometheusRules)

```yaml
# Tasa de error > 5% en 5min
- alert: MoluHighErrorRate
  expr: |
    sum(rate(molu_errors_total[5m])) by (component)
    /
    sum(rate(molu_requests_total[5m])) by (component)
    > 0.05
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Molu error rate > 5% on {{ $labels.component }}"

# Latencia p99 > 1s
- alert: MoluHighLatency
  expr: |
    histogram_quantile(0.99,
      sum(rate(molu_latency_seconds_bucket[5m])) by (component, method, le)
    ) > 1
  for: 5m
  labels:
    severity: warning

# xolu unavailable
- alert: MoluXoluUnavailable
  expr: |
    increase(molu_errors_total{code="XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE"}[5m]) > 0
  for: 1m
  labels:
    severity: critical
```

---

## 6. Health Check (`/healthz`)

### Estados

| Estado | HTTP | Significado |
|--------|------|-------------|
| `ok` | 200 | xolu ping exitoso < 30s |
| `degraded` | 503 | xolu ping > 30s o nunca exitoso |
| `down` | 503 | Proceso apagándose |

### Response JSON
```json
{
  "status": "ok",
  "timestamp": "2026-10-15T14:30:00.123456789Z",
  "checks": {
    "xolu": {
      "status": "available",
      "last_ping": "2026-10-15T14:29:55.123456789Z",
      "seconds_ago": 4.9
    }
  }
}
```

### Cache 30s
- Health check usa **cache TTL 30s** basado en último ping exitoso
- Evita flood de pings a xolu bajo carga de health checks (Kubernetes liveness/readiness)

---

## 7. Rotación de Logs

### Verificación
```bash
# Verificar rotación
ls -la /var/log/molu/
# Debe mostrar: system.log, system.log.20261015-143000.gz, etc.

# Verificar compresión
file /var/log/molu/system.log.20261015-143000.gz
# gzip compressed data
```

### Problemas Comunes
| Síntoma | Causa | Solución |
|---------|-------|----------|
| Logs no rotan | `MAX_SIZE_MB` muy alto o `OUTPUT_PATH` vacío | Verificar config, asegurar `OUTPUT_PATH` seteado |
| Permisos denegados | Usuario no tiene write en directorio | `chown molu:molu /var/log/molu` |
| Disco lleno | Retención muy alta o compresión off | Reducir `MAX_AGE_DAYS`, activar `COMPRESS=true` |

---

## 8. Checklist de Incidencia

```
[ ] 1. Identificar request_id / trace_id del usuario afectado
[ ] 2. Buscar en logs: grep <request_id> /var/log/molu/*.log
[ ] 3. Identificar código de taxonomía en log network/application
[ ] 4. Revisar detalles (details.fields, lastSuccessfulContact, etc.)
[ ] 5. Consultar métricas: rate(molu_errors_total[5m]) por código
[ ] 6. Verificar health check: curl http://localhost:9090/healthz
[ ] 7. Correlacionar con xolu: logs xolu en mismo timestamp
[ ] 7. Aplicar procedimiento según código de taxonomía
[ ] 8. Documentar resolución y actualizar runbook si es nuevo patrón
```

---

## 9. Contactos y Escalamiento

| Nivel | Contacto | SLA |
|-------|----------|-----|
| L1 (Operaciones) | On-call SRE | 15 min |
| L2 (Platform) | Platform team | 1 hora |
| L3 (Desarrollo) | Backend team | 4 horas |

**Documentación relacionada**: 
- [Despliegue](deployment/007-telemetry-deployment.md)
- [Configuración](configuration/007-telemetry-config.md)
- [Diagramas](diagrams/007-telemetry-sequence.mmd)