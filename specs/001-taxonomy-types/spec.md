# Spec 001 — Taxonomy Types (Tipos de la Taxonomía Tipada)

Estado: aprobada

## Contexto y objetivo
Define los seis códigos tipados de resultado obligatorios (§2.4) que molu y el Hub deben emitir sin colapsarlos en errores genéricos. Establece la convención de prefijos: errores propios con `XOLU-MOLU-FRONT-*` y preservación verbatim de códigos `XOLU-*` provenientes de xolu. Cada código requiere una prueba de integración dedicada.

## Usuarios / Actores
- Agente de IA (cliente MCP) — recibe respuestas tipadas.
- molu (frontal MCP) — emite códigos propios y propaga los de xolu.
- Hub de Seam (Registry MCP) — filtra superficie y puede emitir `NOT_OFFERED`.
- xolu (sustrato operacional) — fuente de `XOLU-*` verbatim (guards, disponibilidad).

## Historias de usuario
- HU-1: Como agente, quiero recibir `NOT_FOUND` cuando consulto un objeto inexistente para mi tenant, no un error genérico.
- HU-2: Como agente, quiero recibir `INVALID_TRANSITION` con el diagnóstico exacto de la guarda de xolu cuando una transición FSM es rechazada.
- HU-3: Como operador, quiero que `SUBSTRATE_UNAVAILABLE` incluya último contacto exitoso y próximo reintento para diagnosticar caídas de xolu.
- HU-4: Como desarrollador, quiero tests de integración dedicados por cada código de la taxonomía para garantizar que nunca se colapsan.

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: El Hub usa `Admit`/`Score` para decidir visibilidad; puede emitir `NOT_OFFERED` si función/entidad está fuera de alcance para el tenant/rol.
- **Eje 2 (Qué puede ejecutarse)**: xolu resuelve via FSMs; molu propaga `INVALID_TRANSITION` (guarda verbatim) y `SUBSTRATE_UNAVAILABLE` (con metadatos de reintento).

## Convención de Nombres de Herramienta
- Primitivos genéricos (sin namespace): `walk`, `find`, `get` — pueden emitir cualquiera de los 6 códigos.
- Funciones de dominio (con namespace, ej. `work_orders.CreateOrder`) — pueden emitir cualquiera de los 6 códigos.

## Definiciones
- **Código tipado**: Estructura `{ code: string, message: string, details?: object }` serializada en el campo `error` de la respuesta MCP.
- **Prefijo propio**: `XOLU-MOLU-FRONT-<CODIGO>` (ej. `XOLU-MOLU-FRONT-NOT_FOUND`).
- **Preservación verbatim**: Códigos `XOLU-*` devueltos por xolu se reenvían sin modificar el código ni el mensaje original.
- **Metadatos de reintento**: Para `SUBSTRATE_UNAVAILABLE`, objeto `details` con `lastSuccessfulContact` (RFC3339), `nextRetryAt` (RFC3339), `attemptNumber`, `maxAttempts`.

## Requisitos funcionales (en notación EARS)
- RF-1: CUANDO molu o el Hub detectan que una función/entidad existe pero está fuera de alcance para el tenant/rol actual, EL SISTEMA retorna `NOT_OFFERED` con prefijo `XOLU-MOLU-FRONT-NOT_OFFERED`.
- RF-2: CUANDO se direcciona un objeto que no existe para el tenant, EL SISTEMA retorna `NOT_FOUND` con prefijo `XOLU-MOLU-FRONT-NOT_FOUND` (si molu/Hub lo detectan) o preserva `XOLU-NOT_FOUND` verbatim (si viene de xolu).
- RF-3: CUANDO una consulta válida no arroja resultados, EL SISTEMA retorna `EMPTY` con prefijo `XOLU-MOLU-FRONT-EMPTY` (nunca padding).
- RF-4: CUANDO xolu rechaza una transición FSM, EL SISTEMA retorna `INVALID_TRANSITION` preservando verbatim el código `XOLU-INVALID_TRANSITION` y el mensaje de la guarda original.
- RF-5: CUANDO la entrada incumple el schema JSON del publicador, EL SISTEMA retorna `CONTRACT_VIOLATION` con prefijo `XOLU-MOLU-FRONT-CONTRACT_VIOLATION` listando los campos infractores en `details`.
- RF-6: CUANDO xolu es inalcanzable (timeout, conexión rechazada, 5xx), EL SISTEMA retorna `SUBSTRATE_UNAVAILABLE` con prefijo `XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE` incluyendo `lastSuccessfulContact`, `nextRetryAt`, `attemptNumber`, `maxAttempts` en `details`.
- RF-7: SI xolu devuelve un código `XOLU-*` no listado arriba, EL SISTEMA lo preserva verbatim sin mapeo ni colapso.
- RF-8: EL SISTEMA nunca retorna un error genérico (ej. "internal error") en lugar de uno de los seis códigos tipados.

## Requisitos no funcionales
- Concurrencia segura: cero data races bajo `go test -v -race ./...`.
- Neutralidad de dominio: prohibido usar "Seam" en código, constantes o defaults.
- Go 1.27+, SDK MCP oficial (`github.com/modelcontextprotocol/go-sdk`).
- Tests de integración dedicados por cada uno de los 6 códigos (+ preservación verbatim).

## Casos límite
- xolu caído al arranque: reintentos con backoff (`MOLU_FRONT_PING_FAIL_FLOOR` a `MOLU_FRONT_PING_FAIL_CEILING`) hasta `MOLU_FRONT_STARTUP_MAX_ATTEMPTS`; luego `SUBSTRATE_UNAVAILABLE`.
- Schema JSON del publicador cambiante: validación estricta en cada llamada.
- Carreras de datos entre goroutines que consultan xolu y actualizan catálogo del Hub.

## Fuera de alcance
- Implementación de los primitivos genéricos (`walk`, `find`, `get`) — specs separadas.
- Implementación de la interfaz RPI (`Admit`/`Score`) — specs separadas.
- Transportes MCP (stdio, streamable-http) — spec de despliegue.

## Criterios de finalización
- Todos los RF cubiertos con tests unitarios en verde (`go test -v -race ./...`).
- Pruebas de integración dedicadas para cada fila de la taxonomía (6 tests mínimos + 1 de preservación verbatim).
- Definición de tipos Go compartidos (`pkg/taxonomy/`) usados por molu y Hub.
- Diagrama de secuencia Mermaid (`.mmd` y `.pdf`) para flujo de error tipado.

## Dudas resueltas (ADR-006)
- Paquete `pkg/taxonomy/` interno (no módulo separado).
- Formato `XOLU-<CODE>` fijo para códigos de xolu.
- `RetryMetadata` incluye `attemptNumber`/`maxAttempts`.