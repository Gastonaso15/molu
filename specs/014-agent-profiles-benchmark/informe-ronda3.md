# Informe — Benchmark, tercera ronda: perfil 4 con `molu_help` (spec 014 §7, spec 016)

Fecha: 26-nov-2026. 12 corridas del perfil 4 (6 prompts × 2 repeticiones) en el stack aislado, con el mismo flujo, prompts y verificadores de la segunda ronda (`informe-ronda2.md`), cuyos perfiles 2 y 3 se usan como comparación. Datos en `results/ronda3.jsonl` y `results/raw/`.

**Perfil 4** (idea de Horacio, 25-nov): system prompt fijo ("You are a helpful assistant in logistics and physical assets management…"), preámbulo corto delante de cada petición ("Use the molu MCP tool… When in doubt use molu's molu_help function…") y la herramienta `molu_help`, sin SKILL. Los textos de dominio se cargan desde las carpetas de cada publicador (`pkg/publisher/orders/help/`, `pkg/publisher/issues/help/`) con `MOLU_FRONT_HELP_DIR`; los embebidos en molu son genéricos.

## 1. Resultados

| Perfil | Perfecta/buena | Aceptable | Regular | Tiempo medio (s) | Llamadas | Costo medio (USD) | Costo total (USD) | Corridas que llamaron a `molu_help` |
|---|---|---|---|---|---|---|---|---|
| 2. Seam + xolu | 9/12 | 1 | 2 | 14.8 | 6.4 | 0.082 | 0.99 | n/a |
| 3. + system prompt + SKILL | 10/12 | 0 | 2 | 16.5 | 7.2 | 0.131 | 1.57 | n/a |
| 4. + system prompt + preámbulo + `molu_help` | 9/12 | 1 | 2 | 15.5 | 6.5 | 0.097 | 1.17 | **0 de 12** |

Por prompt: L1, G1, G2 y V1 perfectas en las 2 repeticiones; L2 perfecta y aceptable (una pregunta); V2 regular en ambas (se detiene a pedir el dato obligatorio "recepción confirmada", igual que los perfiles 2 y 3).

## 2. Lectura

1. **El agente no llamó a `molu_help` en ninguna de las 12 corridas**, aunque el preámbulo se lo sugiere ("when in doubt") y la herramienta estaba visible. Las descripciones de las herramientas bastaron para resolver todas las tareas guiadas, así que no hubo "duda". **Este benchmark no demuestra que los textos de ayuda sirvan**: mide el efecto del system prompt y del preámbulo, no el de la ayuda.
2. **Calidad igual que los perfiles 2 y 3** (9/12 contra 9/12 y 10/12), y costo entre ambos (1.17 USD contra 0.99 y 1.57). Una primera pasada del mismo perfil, con los textos embebidos en el binario (que se descartó al mover los textos de dominio a los publicadores), dio 10/12 y 0.91 USD con 0 llamadas a la ayuda: la variación entre pasadas es de ±25 % en costo, del orden de las diferencias entre perfiles. Con 2 repeticiones por celda no se puede afirmar que el perfil 4 abarate nada.
3. **Conclusión**: con este modelo y estas tareas, el perfil 4 es equivalente al 2. Lo que queda por probar es dónde la ayuda debería importar.

## 3. Límites y siguiente
Un solo modelo (el del CLI local) y solo 2 repeticiones; verificador propio; no mide Gemini. Para que `molu_help` tenga ocasión de aportar hay que quitarle al agente otras fuentes de conocimiento: un modelo más débil (el cliente de Gastón), tareas con reglas que las descripciones no cubren, o descripciones de herramientas reducidas. Después, si la ayuda aporta, pasar los textos a xolu como `.seed` (propuesta de Horacio) para editarlos sin compilar.
