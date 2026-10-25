# Spec 006 — Servidor MCP Stdio y Despachador de Primitivos en Molu Frontend

Estado: aprobada (actualizada con los 13 primitivos genéricos §5.1)

## Contexto y objetivo
Actualmente, `cmd/molu/main.go` inicializa la sonda de salud de xolu (`pkg/exec`), el cliente y el cargador de esquemas (`pkg/schema`), y cuenta con el servidor MCP activo sobre transporte `stdio`. Los clientes MCP (agentes de IA e inspectores MCP) descubren (`tools/list`) e invocan (`tools/call`) las herramientas del sustrato operacional xolu.

Esta especificación define el **cableado del servidor MCP sobre transporte `stdio` en `molu`**, registrando dinámicamente la superficie completa de los **13 primitivos genéricos de xolu** (especificados en §5.1 del documento normativo) y despachando las llamadas hacia los endpoints de xolu, con validación estricta de esquemas de entrada (`CONTRACT_VIOLATION`), compuerta de disponibilidad del sustrato (`SUBSTRATE_UNAVAILABLE`), y preservación estricta de la taxonomía tipada de errores.

---

## Usuarios / Actores
- **Agente de IA / MCP Inspector (Cliente MCP)**: Se comunica mediante JSON-RPC sobre `stdio` con `molu`, solicitando `tools/list` y ejecutando `tools/call`.
- **molu (Servidor MCP)**: Servidor que dialoga sobre `stdio`, mapea esquemas a herramientas MCP y despacha invocaciones traduciéndolas a las APIs de xolu.
- **sustrato xolu**: Backend operacional que procesa las operaciones de los primitivos en sus rutas y subsistemas dedicados (entidades v1, FSMs v2, consultas OQL/Sulpher y motor de calendarios).

---

## Los 13 Primitivos Genéricos (§5.1)

Los primitivos genéricos son herramientas desacopladas de dominio que operan directamente sobre el sustrato xolu. **NUNCA llevan namespace**.

### 1. `describe` (§5.1.1)
Lee el mapa semántico/esquemas operativos de xolu. Herramienta principal de orientación del agente.
- **inputSchema**:
  ```json
  {
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
  ```
- **Despacho**: Retorna la porción correspondiente del mapa semántico / catálogo de esquemas serializado como JSON.

### 2. `get` (§5.1.2)
Recupera una entidad específica por tipo e ID.
- **inputSchema**:
  ```json
  {
    "type": "object",
    "required": ["entity_type", "id"],
    "properties": {
      "entity_type": { "type": "string" },
      "id":          { "type": ["string", "integer"] }
    }
  }
  ```
- **Despacho**: `GET /api/v1/{entity_type}/{id}`. Si no existe, emite `NOT_FOUND`.

### 3. `list` (§5.1.3)
Lista entidades de un tipo determinado con filtros de igualdad opcionales, paginación (`limit`, `offset`). Pass-through directo a xolu (molu no post-filtra).
- **inputSchema**:
  ```json
  {
    "type": "object",
    "required": ["entity_type"],
    "properties": {
      "entity_type": { "type": "string" },
      "filter":      { "type": "object", "description": "Field-value equality filters." },
      "limit":       { "type": "integer", "default": 50, "minimum": 1, "maximum": 500 },
      "offset":      { "type": "integer", "default": 0, "minimum": 0 }
    }
  }
  ```
- **Despacho**: `GET /api/v1/{entity_type}` con query params. Si el resultado está vacío, emite `EMPTY` (sin rellenar con padding).

### 4. `create` (§5.1.4)
Crea una nueva entidad de un tipo dado.
- **inputSchema**:
  ```json
  {
    "type": "object",
    "required": ["entity_type", "data"],
    "properties": {
      "entity_type": { "type": "string" },
      "data":        { "type": "object", "description": "Field values for the new entity." }
    }
  }
  ```
- **Despacho**: `POST /api/v1/{entity_type}`. Valida `data` contra el esquema JSON de la entidad; ante violaciones emite `CONTRACT_VIOLATION` con los campos infractores.

### 5. `update` (§5.1.5)
Actualiza campos de una entidad existente, soportando concurrencia optimista (`version`).
- **inputSchema**:
  ```json
  {
    "type": "object",
    "required": ["entity_type", "id", "changes"],
    "properties": {
      "entity_type": { "type": "string" },
      "id":          { "type": ["string", "integer"] },
      "changes":     { "type": "object" },
      "version":     { "type": "integer", "description": "Optimistic concurrency version." }
    }
  }
  ```
- **Despacho**: `PATCH /api/v1/{entity_type}/{id}`. Valida `changes` contra el esquema de la entidad. Si la entidad no existe o hay conflicto de versión, mapea a `NOT_FOUND` o error correspondiente.

### 6. `query` (§5.1.6)
Ejecuta directamente una consulta OQL (tabular) o Sulpher (patrones de grafos).
- **inputSchema**:
  ```json
  {
    "type": "object",
    "required": ["language", "query"],
    "properties": {
      "language": { "type": "string", "enum": ["oql", "sulpher"] },
      "query":    { "type": "string" },
      "params":   { "type": "object", "description": "Optional bind parameters." }
    }
  }
  ```
- **Despacho**: `POST /api/v1/oql/query` o endpoint de consulta Sulpher. molu no analiza la consulta; xolu asume esa responsabilidad. Si no hay filas, emite `EMPTY`.

### 7. `walk` (§5.1.7)
Avanza una máquina de estados FSM mediante un input y payload opcional.
- **inputSchema**:
  ```json
  {
    "type": "object",
    "required": ["machine_id", "input"],
    "properties": {
      "machine_id": { "type": ["string", "integer"] },
      "input":      { "type": "string" },
      "payload":    { "type": "object", "description": "Fields available to guards as payload.<field>." }
    }
  }
  ```
- **Despacho**: `POST /api/v2/fsm/machine/{id}/walk`. Retorna el nuevo estado y salida Mealy. Si xolu rechaza la guarda, emite `INVALID_TRANSITION` preservando el diagnóstico verbatim.

### 8. `machine_state` (§5.1.8)
Lee el estado actual, variables y estatus terminal de una máquina FSM.
- **inputSchema**:
  ```json
  {
    "type": "object",
    "required": ["machine_id"],
    "properties": {
      "machine_id": { "type": ["string", "integer"] }
    }
  }
  ```
- **Despacho**: `GET /api/v2/fsm/machine/{id}/state` y combina con `/vars`. Si la máquina no existe, emite `NOT_FOUND`.

### 9. `machine_history` (§5.1.9)
Lee el historial de transiciones de una máquina FSM.
- **inputSchema**:
  ```json
  {
    "type": "object",
    "required": ["machine_id"],
    "properties": {
      "machine_id": { "type": ["string", "integer"] },
      "limit":      { "type": "integer", "default": 20, "minimum": 1, "maximum": 500 }
    }
  }
  ```
- **Despacho**: `GET /api/v2/fsm/machine/{id}/history`. Si no hay transiciones, emite `EMPTY`.

### 10. `cal_check` (§5.1.10)
Verificación preliminar (*dry-run*) de disponibilidad contra uno o más calendarios sobre un intervalo temporal propuesto, sin reservar.
- **inputSchema**:
  ```json
  {
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
  ```
- **Despacho**: API de calendario de xolu (`POST /api/v2/cal/check`). Retorna disponibilidad o diagnóstico del calendario bloqueante.

### 11. `cal_openings` (§5.1.11)
Encuentra ventanas libres a través de uno o más calendarios según duración, rango y objetivo.
- **inputSchema**:
  ```json
  {
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
  ```
- **Despacho**: API de calendario de xolu (`POST /api/v2/cal/openings`). Si no hay ventanas, emite `EMPTY`.

### 12. `cal_propose` (§5.1.12)
Crea una reserva propuesta en el plano tentativo (*proposed plane*, aún no confirmada).
- **inputSchema**:
  ```json
  {
    "type": "object",
    "required": ["calendars", "start", "end"],
    "properties": {
      "calendars": {
        "type": "array",
        "items": { "type": "string" }
      },
      "start":   { "type": "string", "format": "date-time" },
      "end":     { "type": "string", "format": "date-time" },
      "payload": { "type": "object", "description": "Optional application-defined metadata for the booking." }
    }
  }
  ```
- **Despacho**: API de calendario de xolu (`POST /api/v2/cal/propose`). Retorna el `booking_id`.

### 13. `cal_confirm` (§5.1.13)
Mueve una reserva del plano propuesto al plano vinculante (*binding plane*).
- **inputSchema**:
  ```json
  {
    "type": "object",
    "required": ["booking_id"],
    "properties": {
      "booking_id": { "type": ["string", "integer"] }
    }
  }
  ```
- **Despacho**: API de calendario de xolu (`POST /api/v2/cal/confirm`). Si el `booking_id` no existe, emite `NOT_FOUND`.

---

## Historias de usuario
- **HU-1**: Como **cliente MCP**, quiero ejecutar `tools/list` para recibir la lista completa y actualizada de los 13 primitivos genéricos sin namespace y con sus JSON Schemas validados.
- **HU-2**: Como **cliente MCP**, quiero invocar `tools/call` para cualquiera de los 13 primitivos y recibir la respuesta estructurada o el error tipado correspondiente.
- **HU-3**: Como **agente/operador**, quiero que si los argumentos de un `tools/call` no cumplen el JSON Schema del primitivo, el servidor responda con `CONTRACT_VIOLATION` detallando los campos infractores.
- **HU-4**: Como **agente/operador**, quiero que si xolu no está disponible al momento de invocar una herramienta, el servidor rechace la llamada con `SUBSTRATE_UNAVAILABLE` indicando los metadatos de reintento.
- **HU-5**: Como **cliente MCP**, quiero que si se solicita una herramienta inexistente o fuera de alcance, se retorne `NOT_OFFERED`.

---

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: Molu expone en `tools/list` los 13 primitivos genéricos derivados del sustrato.
- **Eje 2 (Qué puede ejecutarse)**: Al invocar `tools/call`, se valida el contrato de entrada antes de delegar la ejecución a xolu.

---

## Requisitos funcionales (en notación EARS)
- **RF-1**: CUANDO `molu` arranca con transporte `stdio`, EL SISTEMA inicializa el servidor MCP y atiende el ciclo de vida del protocolo (`initialize`, `notifications/initialized`, `ping`).
- **RF-2**: CUANDO un cliente solicita `tools/list`, EL SISTEMA retorna la lista de herramientas con los 13 primitivos genéricos (`describe`, `get`, `list`, `create`, `update`, `query`, `walk`, `machine_state`, `machine_history`, `cal_check`, `cal_openings`, `cal_propose`, `cal_confirm`) sin namespace y con sus esquemas de entrada.
- **RF-3**: CUANDO un cliente invoca `tools/call` para cualquiera de los 13 primitivos, EL SISTEMA valida los argumentos con `ValidateInput()`; SI la entrada es inválida, ENTONCES retorna error tipado **CONTRACT_VIOLATION** con `details.fields[]`; SI es válida, despacha la operación al endpoint correspondiente de xolu:
  - `describe`: retorna las definiciones del mapa semántico / esquemas según el `scope` y `name`.
  - `get`: consulta `GET /api/v1/{entity_type}/{id}` (mapeo a **NOT_FOUND** si no existe).
  - `list`: consulta `GET /api/v1/{entity_type}?{filters}` (mapeo a **EMPTY** si la lista está vacía).
  - `create`: envía `POST /api/v1/{entity_type}` con validación de datos.
  - `update`: envía `PATCH /api/v1/{entity_type}/{id}` con cambios y control de versión.
  - `query`: envía `POST /api/v1/oql/query` o consulta Sulpher (mapeo a **EMPTY** si no hay filas).
  - `walk`: envía `POST /api/v2/fsm/machine/{id}/walk` (si la guarda falla, mapea a **INVALID_TRANSITION** verbatim).
  - `machine_state`: consulta estado y variables de la FSM (mapeo a **NOT_FOUND** si no existe).
  - `machine_history`: consulta transiciones de la FSM (mapeo a **EMPTY** si no hay historial).
  - `cal_check`: ejecuta dry-run de intersección de calendarios.
  - `cal_openings`: consulta ventanas libres (mapeo a **EMPTY** si no hay ventanas).
  - `cal_propose`: registra propuesta y retorna booking ID.
  - `cal_confirm`: confirma reserva y la mueve al binding plane (mapeo a **NOT_FOUND** si no existe).
- **RF-4**: CUANDO un cliente invoca `tools/call`, EL SISTEMA verifica el estado de salud de xolu vía `probe.Check()`; SI xolu no está disponible, ENTONCES retorna error tipado **SUBSTRATE_UNAVAILABLE** con `details.retry_metadata`.
- **RF-5**: CUANDO un cliente invoca `tools/call` para una herramienta desconocida, EL SISTEMA retorna error tipado **NOT_OFFERED**.
- **RF-6**: CUANDO ocurre un refresco de esquemas (por señal `SIGHUP` o ticker periódico), EL SISTEMA actualiza el catálogo de herramientas en el servidor MCP y notifica al cliente según el protocolo MCP (`notifications/tools/list_changed`).
- **RF-7**: EL SISTEMA garantiza concurrencia segura: múltiples llamadas concurrentes a `tools/list` y `tools/call` no producen data races (`go test -v -race ./...` pasa en verde).
- **RF-8**: EL SISTEMA mantiene neutralidad de dominio estricta: cero menciones a "Seam" en el código, constantes o mensajes.

---

## Requisitos no funcionales
- **Compatibilidad**: Go 1.27+, SDK oficial de MCP.
- **Concurrencia segura**: `go test -v -race ./...` debe pasar sin advertencias ni condiciones de carrera.
- **Logs limpios**: En transporte `stdio`, los logs de aplicación (`zerolog`/`slog`) deben redirigirse exclusivamente a `os.Stderr` para no corromper el canal JSON-RPC en `os.Stdout`.

---

## Casos límite
- Cliente envía `tools/list` cuando la carga inicial de schemas aún no ha finalizado: debe responder sin entrar en pánico.
- xolu cae durante la ejecución de `tools/call`: debe atraparse el error y envolverse con `SUBSTRATE_UNAVAILABLE` o preservarse verbatim si proviene de xolu.
- Argumentos nulos, faltantes o tipos incompatibles en `tools/call`: retorno de `CONTRACT_VIOLATION`.

---

## Criterios de finalización
- Servidor MCP escuchando en `stdio` en `cmd/molu/main.go`.
- MCP Inspector / Agente lista los 13 primitivos genéricos con sus descripciones y parámetros.
- Pruebas unitarias y de integración para `tools/list` y `tools/call` cubriendo los 6 errores de la taxonomía (`NOT_OFFERED`, `NOT_FOUND`, `EMPTY`, `INVALID_TRANSITION`, `CONTRACT_VIOLATION`, `SUBSTRATE_UNAVAILABLE`).
- `go test -v -race ./...` 100% verde.
