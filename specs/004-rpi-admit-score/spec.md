# Spec 004 — RPI Admit/Score (Eje 1: Qué se ofrece)

Estado: aprobada

## Contexto y objetivo
Esta especificación define la **interfaz RPI (Repository Provider Interface)** y sus **proveedores V1** (`admit-all` y `static-context-rules`) que resuelven el **Eje 1 de control**: determinar qué herramientas/funciones se ofrecen al agente MCP según el tenant y rol del solicitante. El RPI desacopla la decisión de "qué se ofrece" de la ejecución (Eje 2, FSMs de xolu), permitiendo cambiar la política de admisión y scoring por configuración sin tocar código.

## Usuarios / Actores
- **Agente de IA (cliente MCP)**: Consume herramientas descubiertas vía `tools/list` y `tools/call`.
- **molu (frontal MCP)**: Invoca `Admit` y `Score` del proveedor RPI configurado para filtrar y ordenar el catálogo.
- **Publicador del Hub**: Registra funciones de dominio (`namespace.Nombre`) y primitivos genéricos (`walk`, `find`, `get`) en el catálogo en memoria.
- **Operador/Configuración**: Selecciona proveedor RPI vía variable de entorno `MOLU_FRONT_RPI_PROVIDER`.

## Historias de usuario
- HU-1: Como **agente MCP**, quiero descubrir solo las herramientas admitidas para mi tenant/rol, para no ver funciones fuera de alcance.
- HU-2: Como **operador**, quiero cambiar entre `admit-all` y `static-context-rules` por configuración, para alterar el descubrimiento sin desplegar código nuevo.
- HU-3: Como **publicador**, quiero que mis funciones de dominio (`work_orders.CreateOrder`) y primitivos genéricos (`walk`) pasen por el mismo filtro RPI, para coherencia en la superficie ofrecida.

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: **Principal**. Resuelto íntegramente por la interfaz RPI (`Admit` y `Score`) en molu/Hub. Los proveedores V1 implementan políticas de admisión y puntuación basadas en tenant/rol.
- **Eje 2 (Qué puede ejecutarse)**: **No interviene**. La ejecución (transiciones FSM via `walk`) queda fuera; el RPI solo filtra la superficie de descubrimiento.

## Convención de Nombres de Herramienta
- **Ambos tipos**: El RPI filtra y scorea tanto:
  - **Primitivos genéricos**: NUNCA llevan namespace (ej. `walk`, `find`, `get`).
  - **Funciones de dominio**: SIEMPRE llevan `namespace.Nombre` (ej. `work_orders.CreateOrder`, `inventory.ReserveStock`).

## Definiciones
- **Catálogo en memoria**: Registro de herramientas (`ToolCandidate`) cargado al arranque desde schemas de xolu (spec 003) y publicaciones del Hub.
- **ToolCandidate**: Candidato a herramienta con campos: `name` (string, con o sin namespace), `description`, `inputSchema` (JSON Schema), `primitive` (bool: true para primitivos genéricos), `namespace` (string, vacío para primitivos).
- **Contexto de admisión (`AdmitContext`)**: `{ tenant: string, roles: []string, query?: string }` — información del solicitante para decidir admisión.
- **Proveedor RPI (`RPIProvider`)**: Interfaz con dos métodos puros:
  - `Admit(ctx, context, candidates) -> []ToolCandidate` (subconjunto admitido)
  - `Score(ctx, context, admitted) -> []ScoredTool` (ordenados por relevancia)
- **ScoredTool**: `ToolCandidate` + `score` (float64 0.0–1.0) + `rationale` (string opcional).

## Requisitos funcionales (en notación EARS)
- RF-1: CUANDO molu arranca, EL SISTEMA carga el proveedor RPI indicado en `MOLU_FRONT_RPI_PROVIDER` (`admit-all` | `static-context-rules`).
- RF-2: CUANDO un agente solicita `tools/list`, EL SISTEMA invoca `Admit` con el catálogo completo y el `AdmitContext` del solicitante, y SOLO expone los herramientas admitidas.
- RF-3: SI `Admit` devuelve lista vacía para un tenant/rol válido, ENTONCES EL SISTEMA retorna respuesta `tools/list` vacía (código taxonomía **EMPTY** — consulta válida sin resultados).
- RF-4: SI una herramienta existe en catálogo pero `Admit` la excluye para el tenant/rol, ENTONCES EL SISTEMA NO la expone y la omisión se registra como **NOT_OFFERED** en auditoría (no se retorna al cliente).
- RF-5: CUANDO se expone la lista admitida, EL SISTEMA invoca `Score` para ordenar herramientas por relevancia descendente (score 1.0 = máxima relevancia).
- RF-6: SI el proveedor `static-context-rules` tiene reglas configuradas para el tenant/rol, ENTONCES `Admit` aplica coincidencia exacta de `namespace.Nombre` o primitivo contra reglas permitidas; `Score` asigna 1.0 a permitidas, 0.0 a demás.
- RF-7: SI el proveedor es `admit-all`, ENTONCES `Admit` devuelve todo el catálogo sin filtrar y `Score` asigna 1.0 uniforme a todas.
- RF-8: SI la entrada `AdmitContext` incumple validación básica (tenant vacío), ENTONCES EL SISTEMA retorna **CONTRACT_VIOLATION** con `details.fields: ["tenant"]`.
- RF-9: EL SISTEMA garantiza concurrencia segura: múltiples `tools/list` concurrentes con distintos contextos no producen data races (`go test -v -race ./...` pasa).
- RF-10: EL SISTEMA mantiene neutralidad de dominio: cero referencias a "Seam" en código, constantes, defaults o configuración.

## Requisitos no funcionales
- **Concurrencia segura**: cero data races bajo `go test -v -race ./...`.
- **Neutralidad de dominio**: prohibido usar "Seam" en código, constantes, variables, paquetes o defaults.
- **Configurabilidad**: proveedor RPI seleccionable por `MOLU_FRONT_RPI_PROVIDER` sin recompilar.
- **Rendimiento**: `Admit` + `Score` en < 5ms para catálogo típico (< 200 herramientas).
- **Compatibilidad**: Go 1.27+, SDK MCP oficial.

## Casos límite
- Catálogo vacío al arranque (schemas de xolu no cargados aún): `Admit` recibe lista vacía → respuesta `tools/list` vacía (EMPTY).
- Cambio de proveedor RPI en caliente (SIGHUP o reinicio): se recarga catálogo y proveedor.
- `static-context-rules` sin reglas para un tenant/rol: comportamiento por defecto = denegar todo (lista vacía → EMPTY).
- Tenant/rol con caracteres Unicode: validación y matching case-sensitive.

## Fuera de alcance
- Proveedores RPI avanzados (ML-based, externos, híbridos) — solo V1: `admit-all` y `static-context-rules`.
- Persistencia de decisiones Admit/Score — todo en memoria, stateless.
- Telemetría/métricas del RPI (Prometheus, OpenTelemetry) — solo logs estructurados.
- UI/API de administración para gestionar reglas de `static-context-rules` — reglas solo vía config/env.
- Integración con Hub remoto (catálogo distribuido) — catálogo solo en memoria local.
- Caché de resultados `Admit`/`Score` por contexto — cada llamada recalcula.

## Criterios de finalización
- Todos los RF cubiertos con tests unitarios en verde (`go test -v -race ./...`).
- Pruebas de integración dedicadas para **NOT_OFFERED**, **EMPTY**, **CONTRACT_VIOLATION** (Criterio 2 del proyecto).
- Test de integración que demuestra cambio de proveedor por config altera descubrimiento sin tocar código (Criterio 7).
- Diagrama de secuencia Mermaid (`.mmd` y `.pdf`) para flujos: `tools/list` → Admit → Score → respuesta; y cambio de proveedor por config.

## Dudas resueltas
- [RESUELTO] **Formato exacto de reglas para `static-context-rules`**: Formato JSON cargado desde archivo vía `MOLU_FRONT_RPI_RULES_FILE` con soporte/fallback inline vía variable de entorno `MOLU_FRONT_RPI_RULES_JSON`, parseado mediante la librería estándar `encoding/json` sin dependencias externas.
- [RESUELTO] **Estructura de `AdmitContext`**: Alcance minimalista V1 compuesto por `tenant` (string, obligatorio), `roles` ([]string) y `query` (string opcional). No se añaden atributos ni permisos adicionales en esta fase.