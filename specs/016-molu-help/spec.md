# Spec 016 — `molu_help`: ayuda para agentes dentro de molu

**Estado**: Implementada (26-nov-2026) como primera versión; la ayuda en xolu (`.seed`) está pendiente.
**Origen**: idea de Horacio del 25-nov-2026 en `#seam-cure-ai-subsystem-mcp`: system prompt fijo, un preámbulo corto en cada mensaje y una función `molu_help` con textos que expliquen al agente qué hacer cuando todavía no sabe nada; "parecido al sistema de memorias de Claude más que a los skills"; y, como sugerencia, guardar los textos en xolu como `.seed` para editarlos sin compilar. Reemplaza, para este fin, a la spec 015 (skills sobre MCP, pospuesta).

## 1. Objetivo
Que cualquier cliente MCP pueda pedir a molu una explicación de cómo usarlo y de cómo operar el sistema que hay detrás, por temas, sin depender de skills ni de formatos propios de un cliente.

## 2. Diseño
- **Herramienta `molu_help`** (sin espacio de nombres, como las primitivas), argumento opcional `topic`. Sin tema devuelve la vista general y la lista de temas con su resumen; un tema desconocido devuelve la lista (no es un error); un `topic` que no es texto es `CONTRACT_VIOLATION`. Responde con textos locales, así que funciona aunque xolu o el Hub estén caídos. Se anuncia en `tools/list` solo si la ayuda está activada.
- **Textos** (`pkg/help`): archivos Markdown `<tema>.md`; la primera línea opcional `summary: ...` es el resumen del índice. Los incluidos en el binario son **genéricos** (`overview`, `primitives`, `errors`): por la regla de neutralidad de `AGENTS.md`, molu no contiene lógica ni nombres de dominio.
- **Textos de dominio**: pertenecen a cada publicador y viven en su carpeta (`pkg/publisher/orders/help/`: `work_orders`, `work_orders.execution`, `work_orders.inventory`; `pkg/publisher/issues/help/`: `issues`). Se cargan con `MOLU_FRONT_HELP_DIR`, una lista de carpetas separadas por `:` que se apilan sobre los genéricos (las posteriores ganan) y pueden reemplazar o añadir temas sin recompilar.
- **Configuración**: `MOLU_FRONT_HELP_ENABLED` (por defecto `true`) y `MOLU_FRONT_HELP_DIR`. Una carpeta inválida se registra y se sirven los textos incluidos.

## 3. Verificación
`pkg/help/help_test.go` (temas, resúmenes, neutralidad de los textos incluidos, capas de carpetas, carpetas de los publicadores) y `pkg/mcp/help_test.go` (listado solo si está activa, vista general, temas, tema desconocido, argumento inválido, funciona con el sustrato caído). Benchmark: perfil 4 de la spec 014 (`informe-ronda3.md`): sin diferencias de calidad respecto de los perfiles 2 y 3; **el agente no llamó a `molu_help` en ninguna de las 12 corridas**.

## 4. Pendiente
1. Probar la ayuda donde debería importar: un modelo más débil o tareas con reglas que las descripciones no cubren.
2. **Textos en xolu como `.seed`** (sugerencia de Horacio): leerlos de xolu en vez de carpetas, para editarlos sin compilar ni desplegar y poder variar la ayuda por cliente. Requiere definir la entidad y el formato en la semilla y la recarga en molu.
3. Que los publicadores entreguen sus textos por el Hub al registrarse, en lugar de carpetas del repositorio.
4. Documentar `molu_help` en la guía de despliegue.
