# Spec 013 — Integración OpenUI (Generative UI)

Estado: propuesta

## Contexto y objetivo

Esta especificación define la integración de **OpenUI** al proyecto Molu, permitiendo que los agentes generen interfaces interactivas en formato **OpenUI Lang** en lugar de solo texto o JSON. OpenUI es un framework de "Generative UI" de código abierto (MIT) donde un modelo LLM genera una UI estructurada y streaming usando un lenguaje compacto y componentes definidos previamente.

Esta integración resuelve el deseo de tener respuestas de agente más ricas e interactivas, aprovechando la eficiencia de tokens de OpenUI Lang (hasta 67% menos que JSON) y la capacidad de renderizar componentes interactivos (charts, forms, tables, dashboards) que se ejecutan por sí solos después de la generación inicial.

## Usuarios / Actores

- **Agente de IA (cliente MCP)**: Ahora puede recibir y renderizar respuestas en OpenUI Lang, con componentes interactivos que no requieren roundtrips al LLM para interacciones simples.
- **molu (frontal MCP)**: Se encargado de generar el system prompt a partir del catálogo de herramientas y streamar OpenUI Lang al cliente.
- **Publicador del Hub**: Sus funciones de dominio (`namespace.Nombre`) y primitivos genéricos serán mapeados a componentes OpenUI.
- **Operador/Configuración**: Configura qué componente OpenUI mapea a cada tipo de herramienta.

## Ejes de Control Involucrados

- **Eje 1 (Qué se ofrece)**: **Secundario**. El RPI sigue decidiendo qué herramientas se ofrecen, pero ahora estos se mapean a componentes OpenUI en el system prompt.
- **Eje 2 (Qué puede ejecutarse)**: **Principal**. OpenUI permite que la UI generada ejecute herramientas directamente (`@Run`, `@Set`, `@Reset`) sin necesidad de roundtrips al LLM, separando generación de ejecución.

## Convención de Nombres

- **Componentes OpenUI**: Usarán el mismo nombre que la herramienta Molu, pero adaptados al lenguaje OpenUI Lang.
- **Mapping herramienta → componente**: `ToolCandidate.name` → nombre del componente OpenUI (mayúscula camelCase si tiene namespace, minúscula si es primitivo).
- **Namespacing**: Funciones de dominio con `namespace.Nombre` mapean a `@Namespace.Nombre(...)` en OpenUI Lang; primitivos genéricos (`walk`, `find`, `get`) se mapean directamente.

## Definiciones

- **OpenUI Component Library**: Definición en Go que mapea `ToolCandidate` a firmas de componentes OpenUI. Incluye:
  - Nombre del componente
  - Schema de propiedades (basado en `inputSchema`)
  - Componentes anidados soportados
  - Acciones asociadas (`@Run`, `@Set`, `@Reset`)
- **Tool-to-Component Mapping**: Tabla de decisión que determina qué tipo de componente OpenUI se genera para cada tipo de herramienta:
  - Herramientas de consulta (`Query`) → `Query` component con `@Run`
  - Herramientas de mutación (`Mutation`) → `Mutation` component con `@Run`
  - Primitivos genéricos → Componentes layout básicos (`Stack`, `Card`, etc.)
- **System Prompt Generator**: Función Go que toma el catálogo de `ToolCandidate` y genera un system prompt compatible con `generateSystemPrompt` de `@openuidev/lang-core`.

## Requisitos funcionales (en notación EARS)

- **RF-1**: CUANDO molu recibe una solicitud de tools/list y el modo OpenUI está activado, EL SISTEMA genera un system prompt que incluye el catálogo de herramientas mapeadas a componentes OpenUI Lang.
- **RF-2**: CUANDO el LLM responde con OpenUI Lang, EL SISTEMA lo parsea y evalúa el árbol de elementos resultante, ejecutando `@Run` contra las herramientas admitidas.
- **RF-3**: SI el parseo falla (OpenUI Lang inválido), ENTONCES EL SISTEMA falla gracefully y retorna un mensaje de error estructurado, no un panic.
- **RF-4**: SI el usuario interactúa con un componente generado (ej. button click con `@Run`), ENTONCES EL SISTEMA ejecuta la herramienta correspondiente y retorna el resultado, sin involucrar al LLM en el loop de interacción.
- **RF-5**: CUANDO se genera un sistema prompt, EL SISTEMA incluye definiciones de `$variables` reactivas y `@Count`, `@Sum`, `@Filter` u otras transformaciones de datos soportadas.
- **RF-6**: EL SISTEMA garantiza neutralidad de dominio: cero referencias a "Seam" en el system prompt o definiciones de componentes OpenUI generados.
- **RF-7**: EL SISTEMA soporta concurrencia: múltiples agentes pueden generar OpenUI Lang concurrently sin data races.
- **RF-8**: EL SISTEMA permite configurar OpenUI ON/OFF por vía de variable de entorno `MOLU_FRONT_OPENUI_ENABLED` (default: disabled para no romper comportamiento existente).

## Requisitos no funcionales

- **Concurrencia segura**: cero data races bajo `go test -v -race ./...`. El generador de prompts y parser deben ser thread-safe.
- **Neutralidad de dominio**: prohibido usar "Seam" en código, constantes, variables, defaults o system prompts generados.
- **Configurabilidad**: OpenUI habilitable/deshabilitable por `MOLU_FRONT_OPENUI_ENABLED` sin recompilar (solo cambio de config).
- **Rendimiento**: Generación de system prompt en < 10ms para catálogo típico (< 200 herramientas).
- **Compatibilidad**: Go 1.27+, sin dependencias npm/node en el backend (la parte de renderizado corre en el frontend del cliente).
- **Eficiencia de tokens**: System prompt generado debe ser ≤ 50% del tamaño equivalente en JSON para el mismo catálogo de herramientas.

## Casos límite

- Catálogo vacío al arranque: system prompt genera solo componente root vacío o placeholder.
- Herramienta sin mapping definido: se usa un componente "generic-tool" con descripción de la herramienta.
- OpenUI Lang con errores de sintaxis: parser retorna diagnostics en el ParseResult, el frontend los muestra y falla gracefully.
- Variables `$` no definidas: el evaluador establece valores por defecto o nulos.
- Interacción `@Run` con herramienta no admitida para el tenant actual: el runtime retorna error `NOT_OFFERED` y el componente muestra estado de error.
- Cambio de proveedor RPI en caliente: el mapping de componentes se recalcula con nuevo catálogo.

## Criterios de finalización

- [ ] Spec creada en `specs/013-openui-integration/spec.md`
- [ ] Implementado generador de system prompt Go que toma `[]ToolCandidate` y output `string` de OpenUI Lang
- [ ] Implementado parser básico de OpenUI Lang en Go que puede parsear programas simples
- [ ] Implementado mapeo ToolCandidate → componente OpenUI (mínimo 5 tipos de componentes)
- [ ] Tests unitarios en verde para todas las funciones nuevas (`go test -v -race ./...`)
- [ ] Test de integración que demuestra generación de prompt desde catálogo real de herramientas
- [ ] Diagramas de secuencia Mermaid (`.mmd`) y PDF para flujo: catálogo → system prompt → LLM → OpenUI Lang → renderer
- [ ] Actualizado MEMORY.md con estado de la integración

## Dudas resueltas

- [POR DEFINIR] Formato exacto del system prompt generado desde el catálogo de Molu
- [POR DEFINIR] Qué subset de OpenUI Lang 1.0 se soportará vs qué quedará para futuras fases
- [POR DEFINIR] Cómo el frontend cliente (React/Vue/etc) se integrará con el backend Go
## Verificación del 26-nov-2026

Probado contra los criterios de finalización con un catálogo real de 49 herramientas (13 primitivas, 33 de órdenes, 3 de incidencias). El estado del paquete `pkg/openui` es de **prototipo**: no está conectado a molu (nada lo importa; `tools/list` no lo usa) y `MOLU_FRONT_OPENUI_ENABLED` (RF-8) no existe.

| Criterio / requisito | Resultado |
|---|---|
| Rendimiento: 200 herramientas en < 10 ms | ✅ 5.8 ms (con detector de carreras) |
| RF-7 concurrencia, sin data races | ✅ |
| RF-6 neutralidad (sin el nombre del producto en código ni prompt) | ✅ |
| Casos límite: catálogo vacío, herramienta sin mapeo | ✅ |
| RF-5: variables reactivas y transformaciones en el prompt | ✅ el prompt define `$variables` y las funciones `Count`, `Sum`, `Filter`, `Each` (sin el prefijo `@`) |
| RF-3: una entrada inválida falla con gracia | ❌ → **corregido** (ver abajo) |
| Parser que evalúa programas simples | ⚠️ solo literales y referencias de una statement; no evalúa llamadas a componentes (`Stack([...])`) ni programas de varias statements |
| Mapeo a al menos 5 tipos de componente | ❌ los 49 salen `Generic`: con namespace el prefijo del nombre oculta el verbo (`create`, `list`...) y los tipos Query/Mutation/Card/Table/Chart nunca se asignan; además todos los `get*` se llaman `Query` y pierden su nombre |
| Prompt ≤ 50 % del JSON equivalente | ❌ 95 % (47 261 bytes contra 49 751) |
| Prompt determinista | ❌ se recorre un mapa de funciones sin ordenar |
| Prueba de integración con un LLM | ⚠️ necesita Ollama con `qwen3.5:4b`; no tenía aserciones; ahora se omite si falta el entorno y acepta `OLLAMA_MODEL` |
| RF-1, RF-2, RF-4, RF-8, diagramas, entrada en MEMORY | ❌ pendientes (ver «Pendiente para que funcione») |

**Corregido (26-nov-2026)**: el parser no terminaba con cualquier `$variable` (el tokenizador aceptaba `$` como primer carácter de un identificador pero el bucle de lectura no lo aceptaba, así que la posición no avanzaba y se generaban tokens vacíos sin fin) ni con una entrada que no empezara por un identificador (`(`, `Text("x")`, `foo bar`: `parseStatement` devolvía `nil` sin consumir nada). Como el prompt de molu enseña `$title = ""`, cualquier respuesta con variables colgaba el proceso y agotaba la memoria. Ahora `Parse` consume siempre al menos un token por vuelta, limita el anidamiento (500 niveles) y la entrada (1 MiB) y devuelve errores descriptivos. Pruebas nuevas en `parser_test.go` y `prompt_test.go` (generador, mapeo, neutralidad, rendimiento, concurrencia y 18 entradas hostiles con límite de tiempo); las brechas de la tabla quedan como pruebas omitidas con el motivo a la vista (`*_KnownGap`).

## Pendiente para que funcione (27-nov-2026)

Hoy `pkg/openui` es un prototipo desconectado. Lo que falta, en orden sugerido. Los puntos P1 y P2 se pueden hacer y probar solos; el resto depende de decisiones del equipo.

| # | Tarea | Requisito | Depende de |
|---|---|---|---|
| P1 | **Arreglar el mapeo a componentes**: hoy los 49 salen `Generic`. Con namespace el prefijo oculta el verbo (`create`, `list`...) y nunca se asignan Query/Mutation/Card/Table/Chart; además todos los `get*` se llaman `Query` y pierden su nombre. Levantar los `*_KnownGap` de `prompt_test.go`. | mapeo ≥ 5 tipos | — |
| P2 | **Prompt más chico y determinista**: hoy pesa el 95 % del JSON equivalente (47 261 contra 49 751 bytes) y el requisito es ≤ 50 %. Además se recorre un mapa de funciones sin ordenar: ordenar la salida. | RNF eficiencia de tokens | — |
| P3 | **Parser completo**: evaluar llamadas a componentes (`Stack([...])`) y programas de varias statements; hoy solo literales y referencias de una statement. | RF-2 | — |
| P4 | **Configuración**: `MOLU_FRONT_OPENUI_ENABLED` en `pkg/config`, apagado por defecto, documentado en `docs/configuration.md`. | RF-8 | — |
| P5 | **Conectar a `pkg/mcp`**: generar el prompt desde el catálogo que ofrece el RPI. Decidir cómo se entrega (tool, resource o prompt MCP). | RF-1 | decisión de diseño; Tabaré |
| P6 | **Ejecutar `@Run` sin pasar por el LLM**, por el camino normal de molu (RPI incluido): herramienta no ofrecida → `NOT_OFFERED`. | RF-4, eje 2 | P3, P5 |
| P7 | **Renderer en el cliente** (React u otro): dibuja el árbol y envía los clics. Está fuera de molu; sin él, P1–P6 producen texto que nadie dibuja. | duda abierta del frontend | decisión con Tabaré |
| P8 | **Prueba de integración con Claude además de Ollama**: elegir proveedor con `OPENUI_LLM=claude\|ollama`; con Claude usar `claude -p` (Haiku por defecto, sin API key aparte) o la API con `ANTHROPIC_API_KEY`. Agregar aserciones: lo que devuelve el modelo se parsea sin errores. Seguir omitiéndose si no hay proveedor. Motivo: Ollama es CPU-only en la máquina del equipo y un modelo de 3-4 B no distingue si falla el prompt o el modelo. | criterio de integración | P2 (para gastar menos tokens) |
| P9 | **Cierre de la spec**: diagramas de secuencia Mermaid (`.mmd`) y PDF (catálogo → prompt → LLM → OpenUI Lang → renderer), entrada en `MEMORY.md`. | criterios de finalización | — |

**Decisión previa**: cerrar con Tabaré si se sigue con OpenUI antes de invertir en P5–P7.

**Nota**: molu solo genera el prompt; quien llama al modelo es el cliente MCP (Claude, Gemini, etc.). Por eso el modelo concreto solo importa para la prueba de integración (P8).
