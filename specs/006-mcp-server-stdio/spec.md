# Spec 006 — Servidor MCP Stdio y Despachador de Primitivos en Molu Frontend

Estado: aprobada


## Contexto y objetivo
Actualmente, `cmd/molu/main.go` inicializa la sonda de salud de xolu (`pkg/exec`), el cliente y el cargador de esquemas (`pkg/schema`), pero no cuenta con el bucle de servidor MCP activo. Como consecuencia, clientes MCP como el MCP Inspector no pueden descubrir (`tools/list`) ni invocar (`tools/call`) las herramientas del sustrato.

Esta especificación define el **cableado del servidor MCP sobre transporte `stdio` en `molu`**, registrando dinámicamente los primitivos genéricos de xolu (`walk`, `find`, `get`) y despachando las llamadas con validación estricta de esquemas (`CONTRACT_VIOLATION`) y compuerta de disponibilidad del sustrato (`SUBSTRATE_UNAVAILABLE`).

## Usuarios / Actores
- **Agente de IA / MCP Inspector (Cliente MCP)**: Se comunica mediante JSON-RPC sobre `stdio` con `molu`, solicitando `tools/list` y ejecutando `tools/call`.
- **molu (Servidor MCP)**: Servidor que dialoga sobre `stdio`, mapea esquemas a herramientas MCP y despacha invocaciones.
- **sustrato xolu**: Backend operacional que procesa las operaciones de los primitivos (`walk`, `find`, `get`).

## Historias de usuario
- HU-1: Como **cliente MCP**, quiero ejecutar `tools/list` para recibir la lista actualizada de primitivos genéricos de xolu con sus nombres sin namespace y sus contratos JSON Schema.
- HU-2: Como **cliente MCP**, quiero invocar `tools/call` para un primitivo genérico (`walk`, `find`, `get`) y recibir el resultado estructurado de la operación.
- HU-3: Como **agente/operador**, quiero que si los argumentos de un `tools/call` no cumplen el schema JSON, el servidor responda con el código tipado `CONTRACT_VIOLATION` detallando los campos infractores.
- HU-4: Como **agente/operador**, quiero que si xolu no está disponible al momento de invocar una herramienta, el servidor rechace la llamada con `SUBSTRATE_UNAVAILABLE` indicando los metadatos de reintento.
- HU-5: Como **cliente MCP**, quiero que si se solicita una herramienta inexistente o fuera de alcance, se retorne `NOT_OFFERED`.

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: Molu expone en `tools/list` los primitivos genéricos descubiertos por `SchemaLoader`.
- **Eje 2 (Qué puede ejecutarse)**: Al invocar `tools/call`, se valida el contrato de entrada antes de delegar la ejecución de transiciones FSM a xolu (`walk`).

## Convención de Nombres de Herramienta
- **Primitivos genéricos**: NUNCA llevan namespace (estrictamente `walk`, `find`, `get`).
- **Funciones de dominio**: No se registran directamente en el core de este frontend (pertenecen al catálogo del Hub).

## Definiciones
- **MCP Server Stdio**: Bucle de escucha JSON-RPC que lee de `os.Stdin` y escribe en `os.Stdout`.
- **Primitive Tool**: Herramienta MCP construida a partir de `schema.PrimitiveSchema` (`Name`, `Description`, `InputSchema`).
- **Gating de Disponibilidad**: Verificación mediante `probe.Check()` antes de procesar una herramienta. Si falla, se interrumpe y se emite `SUBSTRATE_UNAVAILABLE`.

## Requisitos funcionales (en notación EARS)
- RF-1: CUANDO `molu` arranca con transporte `stdio`, EL SISTEMA inicializa el servidor MCP y atiende el ciclo de vida del protocolo (`initialize`, `ping`).
- RF-2: CUANDO un cliente solicita `tools/list`, EL SISTEMA retorna todos los primitivos genéricos obtenidos de `SchemaLoader.GetPrimitives()`, preservando sus nombres sin namespace, descripciones y esquemas de entrada.
- RF-3: CUANDO un cliente invoca `tools/call` para un primitivo, EL SISTEMA valida los argumentos con `schema.ValidateInput()`; SI la entrada es inválida, ENTONCES retorna error tipado **CONTRACT_VIOLATION** con `details.fields[]`.
- RF-4: CUANDO un cliente invoca `tools/call`, EL SISTEMA verifica el estado de salud de xolu vía `probe.Check()`; SI xolu no está disponible, ENTONCES retorna error tipado **SUBSTRATE_UNAVAILABLE** con `details.retry_metadata`.
- RF-5: CUANDO un cliente invoca `tools/call` para una herramienta desconocida, EL SISTEMA retorna error tipado **NOT_OFFERED**.
- RF-6: CUANDO ocurre un refresco de esquemas (por señal `SIGHUP` o ticker periódico), EL SISTEMA actualiza el catálogo de herramientas en el servidor MCP y notifica al cliente según el protocolo MCP (`notifications/tools/list_changed`).
- RF-7: EL SISTEMA garantiza concurrencia segura: múltiples llamadas concurrentes a `tools/list` y `tools/call` no producen data races (`go test -v -race ./...` pasa en verde).
- RF-8: EL SISTEMA mantiene neutralidad de dominio estricta: cero menciones a "Seam" en el código, constantes o mensajes.

## Requisitos no funcionales
- **Compatibilidad**: Go 1.27+, SDK oficial de MCP.
- **Concurrencia segura**: `go test -v -race ./...` debe pasar sin advertencias ni condiciones de carrera.
- **Logs limpios**: En transporte `stdio`, los logs de aplicación (`slog`) deben redirigirse exclusivamente a `os.Stderr` para no corromper el canal JSON-RPC en `os.Stdout`.

## Casos límite
- Cliente envía `tools/list` cuando la carga inicial de schemas aún no ha finalizado o el catálogo está vacío: debe responder lista vacía de herramientas (`[]`) sin entrar en pánico.
- xolu cae durante la ejecución de `tools/call`: debe atraparse el error y envolverse con `SUBSTRATE_UNAVAILABLE` o preservarse verbatim si proviene de xolu.
- Argumentos nulos o formato JSON inválido en `tools/call`: retorno de `CONTRACT_VIOLATION`.

## Fuera de alcance
- Transporte HTTP/SSE (definido en entregables posteriores para despliegue en red).
- Autenticación multitenant con JWT sobre stdio (stdio asume canal local seguro para el agente).

## Criterios de finalización
- Servidor MCP escuchando en `stdio` en `cmd/molu/main.go`.
- MCP Inspector lista las herramientas `walk`, `find`, `get` con sus descripciones y parámetros.
- Pruebas unitarias y de integración para `tools/list` y `tools/call` cubriendo `CONTRACT_VIOLATION`, `SUBSTRATE_UNAVAILABLE` y `NOT_OFFERED`.
- `go test -v -race ./...` 100% verde.

## Dudas abiertas / Clarificaciones
- [RESUELTO] **Esquemas de respaldo prohibidos**: No se debe cargar ningún conjunto de esquemas de respaldo (fallback o mock padding). `molu` depende estrictamente de que `xolu` exponga su catálogo de esquemas en vivo; si `xolu` no tiene esquemas o no responde, `molu` aplica su política de reintentos y error sin inventar primitivos.

