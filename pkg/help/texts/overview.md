summary: qué es molu, qué grupos de funciones ofrece y cómo trabajar con ellas
# molu

molu es la herramienta por la que operas un sistema de gestión de activos físicos. Todo lo que hagas sobre el sistema lo haces con las funciones de molu.

## Qué funciones tiene
1. **Primitivas genéricas** (`list`, `get`, `query`, `create`, `update`, `walk`, `describe`...): leen y escriben cualquier entidad de la base. Sirven sobre todo para consultar. Tema: `primitives`.
2. **Funciones de dominio**, cuyo nombre lleva un punto entre el módulo y la función: las publica cada módulo del sistema y aplican sus reglas. **Usa solo los nombres exactos que aparecen en la lista de herramientas disponibles**; no inventes nombres. Los temas de esta ayuda que no son genéricos las explican.

## Cómo trabajar
- Prefiere las funciones de dominio a las primitivas para **escribir**: las primitivas guardan lo que reciben sin validar reglas del sistema.
- Si un dato se puede deducir consultando (`List*` / `Get*`), consúltalo en vez de preguntar. Pregunta solo lo que ninguna consulta puede darte, por ejemplo un valor que debe decidir una persona.
- Si una función rechaza lo que pides, lee el motivo: casi siempre dice qué falta o qué paso va antes. Tema: `errors`.
- Al terminar, resume lo que quedó hecho: ids, estados y cantidades.

Llama a `molu_help` con un tema cuando dudes. Abajo está la lista de temas disponibles.
