# Spec 004 — RPI Admit & Score (Eje 1: Qué se ofrece)

Estado: implementada

## Contexto y objetivo

Esta especificación define la **interfaz RPI (Relevance Provider Interface)** y sus **proveedores V1** para el **Eje 1 de control** (§2.2): decidir **qué funciones de dominio se ofrecen al agente** según el tenant y rol que consulta. La RPI opera sobre el catálogo del Hub (referencias `namespace.Nombre`, no copias) y produce una lista admitida y ordenada que el frontal MCP expone al cliente. No toca el Eje 2 (ejecución vía FSMs de xolu / primitivo `walk`).

## Usuarios / Actores

- **Agente de IA (cliente MCP)**: consulta herramientas disponibles; recibe solo las admitidas por la RPI.
- **Publicador del Hub**: registra funciones de dominio (`namespace.Nombre`) en el catálogo.
- **Operador / Configuración**: selecciona proveedor RPI y reglas por variable de entorno.

## Historias de usuario

- HU-1: Como **agente**, quiero recibir solo las funciones de dominio que mi tenant/rol puede usar, para no ver opciones inalcanzables.
- HU-2: Como **operador**, quiero cambiar el proveedor RPI por configuración (`admit-all` ↔ `static-context-rules`) sin recompilar, y que el descubrimiento cambie en consecuencia (Criterio 7).
- HU-3: Como **desarrollador**, quiero un test double de `Score` para validar el contrato RPI en pruebas de integración.

## Ejes de Control Involucrados

- **Eje 1 (Qué se ofrece)**: **Foco exclusivo de esta spec**. La interfaz RPI (`Admitter`, `Scorer`) y sus proveedores V1 filtran y ordenan candidatos del catálogo del Hub según tenant/rol. Los primitivos genéricos sin namespace (`walk`, `find`, `get`) **no pasan por la RPI**.
- **Eje 2 (Qué puede ejecutarse)**: **Fuera de alcance**. Determinado por FSMs de xolu invocadas vía primitivo genérico `walk`; incluye `INVALID_TRANSITION` y `SUBSTRATE_UNAVAILABLE` de ejecución.

## Convención de Nombres de Herramienta

- **Funciones de dominio (candidatos RPI)**: **SIEMPRE** con `namespace.Nombre` (ej. `work_orders.CreateOrder`, `inventory.Reserve`).
- **Primitivos genéricos**: NUNCA llevan namespace (`walk`, `find`, `get`) — **no son procesados por la RPI**.

## Definiciones

- **Candidate**: Referencia a una función de dominio del catálogo — identificador `namespace.Nombre` + metadatos mínimos (descripción opcional). **No incluye schemas ni payloads copiados** (Entregables §2.5: referencias, no copias).
- **AdmitQuery**: Contexto de quién pregunta — `tenant` (string), `role` (string), `originalQuery` (string, texto libre del agente).
- **AdmittedCandidate**: Candidate + `admissionResult` (`Admitted` | `NotOffered` con razón). El orden de la lista es el orden de presentación al agente.
- **ScoreQuery**: Contexto de scoring — hereda `tenant`, `role`, `originalQuery` de `AdmitQuery`.
- **ScoredCandidate**: **Es un `AdmittedCandidate` reordenado**. **No expone `score` ni `rationale`** (Entregables §2.5: ningún tipo interno del proveedor atraviesa la interfaz; de la RPI solo salen referencias y su orden).
- **RPIProvider**: Implementación concreta que provee `Admitter` (obligatorio) y opcionalmente `Scorer`.
- **Rol**: Es el rol del actor que consulta (Entregables §2.5: "tenant y rol del actor"). Es un **string opaco** que llega en el contexto de `Admit`; molu **no define ni valida** una lista de roles (neutralidad de dominio). **NO es** el rol publisher/consumer de las credenciales del Hub (Parte 3 §5.1). Cómo se obtiene el rol del actor queda fuera de esta spec (wiring con el Hub).
- **static-context-rules**: Asocia pares `(tenant, rol)` con las funciones permitidas. Si un par `(tenant, rol)` no tiene regla, sus candidatos quedan `NOT_OFFERED` (denegar por defecto).

## Requisitos funcionales (en notación EARS)

- RF-1: CUANDO se invoca `Admit` sobre el proveedor seleccionado por configuración, EL SISTEMA le pasa el contexto (tenant, rol, consulta original) y la lista completa de candidatos.
- RF-2: SI un candidato no pasa el filtro de tenant/rol del proveedor, ENTONCES EL SISTEMA lo marca explícitamente con `NOT_OFFERED` (incluye razón) y **no** lo elimina en silencio.
- RF-3: SI tras el filtrado no queda ningún candidato admitido, ENTONCES EL SISTEMA devuelve lista vacía (`EMPTY` — resultado válido, sin padding, sin error).
- RF-4: SI el proveedor RPI reporta que no está disponible, ENTONCES EL SISTEMA retorna `SUBSTRATE_UNAVAILABLE` con `lastSuccessfulContact` y `nextRetryAt`; **nunca** devuelve lista vacía que el agente confundiría con "no hay candidatos".
- RF-5: EL SISTEMA permite seleccionar el proveedor RPI mediante variable de entorno `MOLU_FRONT_RPI_PROVIDER` con valores `admit-all` | `static-context-rules` (default: `static-context-rules`). Valor desconocido → molu no arranca.
- RF-6: EL PROVEEDOR `admit-all` implementa `Admitter` admitiendo **todos** los candidatos sin filtrar (orden estable, sin `Score`).
- RF-7: EL PROVEEDOR `static-context-rules` implementa `Admitter` filtrando por tenant/rol con **reglas estáticas cargadas al arranque** desde configuración (ej. `MOLU_FRONT_RPI_RULES_FILE`); orden estable, **sin `Score`**. Formato de reglas se decide en plan.md.
- RF-8: EL **test double** `score-test-double` implementa `Scorer` (y opcionalmente delega `Admit`) **solo para tests**; **no** es seleccionable por `MOLU_FRONT_RPI_PROVIDER`.
- RF-9: LA INTERFAZ `Admitter` recibe `AdmitQuery` y `[]Candidate`, devuelve `[]AdmittedCandidate` (cada uno con su resultado) y `error` (solo para `SUBSTRATE_UNAVAILABLE` del proveedor).
- RF-10: LA INTERFAZ `Scorer` (opcional) recibe `ScoreQuery` y `[]AdmittedCandidate` **ya admitidos para ese tenant**, devuelve `[]AdmittedCandidate` **reordenados** y `error`. **Nunca** puntúa candidatos de otro tenant (partición antes de puntuar).
- RF-11: EL SISTEMA es **stateless** en persistencia propia: la RPI no guarda decisiones, no es segunda fuente de verdad, devuelve solo referencias e identificadores.
- RF-12: EL SISTEMA **nunca** usa la palabra "Seam" en código, constantes, configuración por defecto ni mensajes de error (neutralidad de dominio, §2.6 punto 4).
- RF-13: CADA PROVEEDOR declara, al registrarse, qué etapas implementa (`Admit`, `Score`, o ambas). El registro de proveedores expone esta capacidad.
- RF-14: CUANDO un candidato resulta `NotOffered`, EL SISTEMA provee una conversión a error tipado de taxonomía mediante `taxonomy.NewNotOffered(tenant, candidateID)`, produciendo código `XOLU-MOLU-FRONT-NOT_OFFERED`. El test de integración verifica ese código exacto y que **no** es `NOT_FOUND`.

## Requisitos no funcionales

- Concurrencia segura: `go test -v -race ./...` pasa sin data races.
- Go 1.27+, SDK oficial MCP (`github.com/modelcontextprotocol/go-sdk`).
- Interfaces `Admitter` y `Scorer` desacopladas para testing con mocks.
- Configuración válida al arranque: variables requeridas presentes, formato de reglas correcto, proveedor conocido.
- Rendimiento: `Admit`/`Score` deben ser rápidos (operación en memoria, sin I/O de red).
- **Idioma**: todos los identificadores, tipos, funciones, comentarios y tests en **inglés**; el texto de specs, planes, tareas y documentación en **español** (AGENTS.md).

## Casos límite

- Catálogo vacío → `EMPTY` (lista admitida vacía, no error).
- Proveedor `static-context-rules` sin archivo de reglas o formato inválido → molu no arranca.
- Tenant/rol no presente en reglas → candidato marcado `NOT_OFFERED`.
- Cambio de proveedor por config sin reinicio: **fuera de alcance** (requiere reinicio en V1).
- Reglas con tenant/rol comodín (`*`) → se decide en plan.md.
- **Tenant/rol sin regla en `static-context-rules` → `Offered` vacío, `err == nil`, todos los candidatos en `NotOffered` con `Reason` no vacío**.

## Fuera de alcance

- Proveedores RPI avanzados (embeddings, base vectorial, LLM, políticas dinámicas) — solo se entrega test double de `Score` con nota de diseño en plan.md que muestre extensibilidad.
- Cacheo de resultados de `Admit`/`Score`.
- Telemetría, auditoría, métricas de decisiones RPI (spec propia §3.4).
- Hot-reload de reglas de `static-context-rules` (se cargan al arranque; SIGHUP no recarga reglas en V1).
- Wiring con el Hub/Registry MCP y registro de herramientas MCP (spec del Hub).
- Persistencia de decisiones de admisión/scoring (RPI es stateless).
- `NOT_FOUND`, `CONTRACT_VIOLATION`, `INVALID_TRANSITION` (no aplican en Eje 1 RPI).
- `score-test-double` como valor de `MOLU_FRONT_RPI_PROVIDER` (solo vive en tests).

## Criterios de finalización

- Interfaces `Admitter` y `Scorer` definidas en `pkg/rpi/`.
- Proveedores V1 implementados: `admit-all`, `static-context-rules` (con reglas desde config), test double de `Score`.
- Variable `MOLU_FRONT_RPI_PROVIDER` operativa (cambio sin tocar código altera descubrimiento — Criterio 7).
- Tests unitarios: `Admitter` de ambos proveedores, `Scorer` del test double, carga de reglas, validación config.
- Test de integración dedicado para `NOT_OFFERED` (candidato filtrado por tenant/rol) y `EMPTY` (lista admitida vacía).
- Test de integración para `SUBSTRATE_UNAVAILABLE` (proveedor no disponible — probado con proveedor falso en tests).
- **Pruebas de conformidad §2.5 contra AMBOS proveedores**: verifican que (a) devuelven referencias y no copias; (b) **vacío honesto sin padding: caso `(tenant, rol)` sin regla → `Offered` vacío, `err == nil`, todos los candidatos en `NotOffered` con `Reason` no vacío**; (c) cada rechazado queda marcado `NOT_OFFERED`; (d) proveedor no disponible da error tipado y no lista vacía; (e) ningún tipo interno sale de la interfaz.
- **Test del Criterio 7**: mismo contexto y candidatos, `admit-all` vs `static-context-rules` elegidos por `MOLU_FRONT_RPI_PROVIDER` dan resultados distintos.
- **Test de integración `NOT_OFFERED` verifica código taxonomía exacto `XOLU-MOLU-FRONT-NOT_OFFERED` y que no es `NOT_FOUND`**.
- Diagrama de secuencia Mermaid (`.mmd` + `.pdf`): flujo descubrimiento → RPI Admit → resultado admitido/NOT_OFFERED/EMPTY/SUBSTRATE_UNAVAILABLE.
- `go test -v -race ./...` pasa limpio (cero data races).

## Decisiones que se toman en plan.md

- Formato exacto del archivo de reglas `MOLU_FRONT_RPI_RULES_FILE` (JSON/YAML/TOML, estructura).
- Soporte de comodines (`*`) para tenant/rol en `static-context-rules`.
- Función helper `NotOfferedToTaxonomyError(tenant string, admittedCandidate AdmittedCandidate) *taxonomy.TypedError` en `pkg/rpi` para convertir `NotOffered` a error tipado.
- Traducción de comentarios en español en `pkg/rpi/*.go` al inglés (AGENTS.md).