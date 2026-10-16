# Despliegue de Telemetría y Observabilidad (Spec 007)

## Visión General

Este documento describe el despliegue operativo del subsistema de telemetría y observabilidad de **molu** (frontal MCP) y **Hub de Seam** (Registry MCP), conforme a la Spec 007.

## Arquitectura de Despliegue

```
┌─────────────────────────────────────────────────────────────────┐
│                     Molu Frontal MCP                            │
├─────────────────────────────────────────────────────────────────┤
│  Puerto MCP (ej. 8080)          Puerto Telemetry (9090)        │
│  ┌─────────────────┐           ┌─────────────────┐             │
│  │ MCP Transport   │           │ HTTP Server     │             │
│  │ (stdio / HTTP)  │           │ /metrics        │             │
│  └────────┬────────┘           │ /healthz        │             │
│           │                    └────────┬────────┘             │
│           ▼                             │                      │
│  ┌─────────────────────────────────────────────────┐           │
│  │ Telemetry Middleware (request_id, trace_id)    │           │
│  └────────────────────┬────────────────────────────┘           │
│                       ▼                                         │
│  ┌─────────────────────────────────────────────────┐           │
│  │ Logger (4 dominios) + Metrics + Health          │           │
│  └─────────────────────────────────────────────────┘           │
└─────────────────────────────────────────────────────────────────┘
```

## Orden Tolerado de Arranque

El despliegue debe seguir este orden para garantizar disponibilidad:

1. **Infraestructura base**
   - Sistema de archivos para logs (directorio `MOLU_FRONT_LOG_OUTPUT_PATH`)
   - Permisos de escritura en directorio de logs
   - Puerto 9090 disponible para telemetría

2. **Servicio de telemetría (puerto 9090)**
   - Iniciar servidor HTTP en `MOLU_FRONT_TELEMETRY_PORT` (default 9090)
   - Endpoints disponibles inmediatamente:
     - `GET /metrics` — Métricas Prometheus
     - `GET /healthz` — Health check (liveness/readiness)

3. **Servicio MCP (puerto configurado)**
   - Iniciar transporte MCP (stdio o streamable-http)
   - Registrar primitivas genéricas (`walk`, `find`, `get`)
   - Conectar a xolu (con backoff y reintentos)

4. **Verificación post-arranque**
   - `GET http://localhost:9090/healthz` → 200 OK
   - `GET http://localhost:9090/metrics` → métricas expuestas
   - Log de sistema nivel INFO con versión, build info, flags efectivos

## Configuración de Puertos

| Puerto | Servicio | Variable | Default |
|--------|----------|----------|---------|
| MCP | Transporte MCP (stdio/HTTP) | `MOLU_FRONT_PORT` | 8080 |
| Telemetry | Métricas + Health | `MOLU_FRONT_TELEMETRY_PORT` | 9090 |
| xolu | Conexión a sustrato | `MOLU_FRONT_XOLU_URL` | — |

**Regla**: El puerto de telemetría (9090) **debe ser distinto** del puerto MCP para evitar contención y exponer métricas solo a sistemas de monitoreo internos.

## Reintentos con Backoff (Conexión a xolu)

Al arrancar, molu intenta conectar a xolu con backoff exponencial:

| Parámetro | Variable | Default | Descripción |
|-----------|----------|---------|-------------|
| Intervalo base | `MOLU_FRONT_PING_INTERVAL` | 10s | Intervalo entre pings |
| Timeout ping | `MOLU_FRONT_PING_TIMEOUT` | 5s | Timeout por intento |
| Floor backoff | `MOLU_FRONT_PING_FAIL_FLOOR` | 1s | Backoff mínimo |
| Ceiling backoff | `MOLU_FRONT_PING_FAIL_CEILING` | 30s | Backoff máximo |
| Max intentos inicio | `MOLU_FRONT_STARTUP_MAX_ATTEMPTS` | 10 | Reintentos antes de fallar |

Si se agotan `MAX_ATTEMPTS`, molu emite log `SUBSTRATE_UNAVAILABLE` con `RetryMetadata` y sale con código 1.

## Transportes MCP Soportados

| Transporte | Descripción | Caso de uso |
|------------|-------------|-------------|
| `stdio` | stdin/stdout | Desarrollo, testing, sidecar |
| `streamable-http` | HTTP/1.1 + SSE | Producción, load balancer |

Ambos transportes comparten el mismo middleware de telemetría (request_id, trace_id, métricas).

## Logs y Rotación

| Archivo | Dominio | Rotación | Retención | Compresión |
|---------|---------|----------|-----------|------------|
| `system.log` | Sistema | 100 MB / 7 días | 7 días | Sí (gzip) |
| `application.log` | Aplicación | 100 MB / 7 días | 7 días | Sí (gzip) |
| `network.log` | Red/Acceso | 100 MB / 7 días | 7 días | Sí (gzip) |
| `security.log` | Seguridad | 100 MB / 90 días | 90 días | Sí (gzip) |

**Seguridad**: `security.log` usa **flush síncrono** (fsync en cada write) para integridad de auditoría.

## Variables de Entorno Críticas para Despliegue

```bash
# Logging
MOLU_FRONT_LOG_FORMAT=json          # json | console
MOLU_FRONT_LOG_LEVEL=info           # debug | info | warn | error
MOLU_FRONT_LOG_OUTPUT_PATH=/var/log/molu
MOLU_FRONT_LOG_MAX_SIZE_MB=100
MOLU_FRONT_LOG_MAX_AGE_DAYS=7
MOLU_FRONT_LOG_COMPRESS=true
MOLU_FRONT_SECURITY_LOG_MAX_AGE_DAYS=90

# Telemetría
MOLU_FRONT_TELEMETRY_PORT=9090
MOLU_FRONT_TELEMETRY_METRICS_PATH=/metrics
MOLU_FRONT_TELEMETRY_HEALTH_PATH=/healthz
MOLU_FRONT_TELEMETRY_ENABLED=true

# Conexión xolu
MOLU_FRONT_XOLU_URL=http://xolu:8080
MOLU_FRONT_PING_INTERVAL=10s
MOLU_FRONT_PING_TIMEOUT=5s
MOLU_FRONT_PING_FAIL_FLOOR=1s
MOLU_FRONT_PING_FAIL_CEILING=30s
MOLU_FRONT_STARTUP_MAX_ATTEMPTS=10
```

## Verificación Post-Despliegue

```bash
# 1. Health check
curl -f http://localhost:9090/healthz
# Expected: 200 OK, JSON con status "ok" y xolu "available"

# 2. Métricas
curl -f http://localhost:9090/metrics
# Expected: 200 OK, formato Prometheus con molu_* métricas

# 3. Logs recientes
tail -f /var/log/molu/system.log
# Expected: logs JSON con timestamp, level, domain, component

# 4. Test MCP (si streamable-http)
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"method":"tools/list","params":{}}'
```

## Apagado Gracioso (Graceful Shutdown)

Al recibir `SIGTERM`/`SIGINT`:

1. Deja de aceptar nuevas conexiones MCP
2. Espera a que terminen requests en curso (timeout configurable)
3. Emite log de sistema nivel INFO con:
   - Uptime total
   - Contadores de requests procesados
   - Razón de apagado
4. Cierra servidor telemetry HTTP (timeout 5s)
5. Cierra archivos de log (flush final)
6. Exit code 0

## Checklist de Despliegue

- [ ] Directorio logs creado y writable
- [ ] Puerto 9090 libre
- [ ] Variables de entorno configuradas
- [ ] xolu accesible (health check previo)
- [ ] Health check `/healthz` retorna 200
- [ ] Métricas `/metrics` expuestas
- [ ] Logs rotando correctamente
- [ ] Security log con flush síncrono verificado
- [ ] Apagado gracioso testeado