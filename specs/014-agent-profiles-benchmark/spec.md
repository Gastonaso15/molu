# Spec 014 — Benchmark de perfiles de agente sobre molu (Work Orders)

## 1. Objetivo
Responder con datos (pedido de Horacio, 24-nov-2026) si un agente necesita conocimiento previo de Seam
(código fuente, SKILL, system prompt) para resolver tareas de Work Orders vía MCP, y cuánto cambia el
tiempo de respuesta. El código fuente de Seam NO es input de ningún perfil.

## 2. Perfiles
| # | Perfil | Superficie MCP | Contexto extra |
|---|---|---|---|
| 1 | Agente pelado | molu sin Hub: solo los 13 primitivos de xolu | ninguno |
| 2 | Seam + Xolu | molu + Hub: 13 primitivos + 30 `work_orders.*` | ninguno |
| 3 | Seam + Xolu + priming | igual que 2 | system prompt de priming + SKILL `work-orders-seam` |
| 4 | Seam + Xolu + `molu_help` | igual que 2 + la herramienta `molu_help` | system prompt fijo + preámbulo corto en cada petición (idea de Horacio, spec 016); sin SKILL |

Control común: mismo modelo, carpeta de trabajo vacía fuera del repo, sin herramientas de archivos,
sin memoria ni settings del usuario, `demo-reset.sh` antes de cada corrida que muta estado.

## 3. Prompts (los mismos en los tres perfiles)
| Id | Tipo | Resumen |
|---|---|---|
| L1 | lectura | plantillas existentes y estados de la de traslado |
| L2 | lectura | consumibles con stock bajo y dónde |
| G1 | guiada | crear orden de traslado asignada a la cuadrilla, confirmar la cabecera, ejecutar el bloque con sus respuestas y llevarla al estado final |
| G2 | guiada | orden de mantenimiento con consumibles: entrada de stock, carga al almacén móvil, ejecución completa hasta el estado final y liquidación (usado 1, devolver el resto) |
| V1 | vaga | "hay stock bajo de algo, reponelo" |
| V2 | vaga | "hay una orden en el tablero sin dueño, dejala en manos de quien corresponda y avanzala" |
Texto exacto en `run.py`.

## 4. Métricas por corrida
Tiempo total (s), tiempo de API (s), vueltas del modelo, llamadas a herramientas, llamadas con error,
costo (USD), si usó el SKILL, si hizo preguntas al usuario, y **calidad** (perfecta/buena/aceptable/regular/mala).

## 5. Rúbrica de calidad (automática, sobre el estado final de xolu)
- **perfecta**: objetivo cumplido, sin preguntas al usuario, ≤ 2 llamadas con error.
- **buena**: objetivo cumplido, sin preguntas, más de 2 errores.
- **aceptable**: objetivo cumplido pero preguntó algo al usuario.
- **regular**: objetivo cumplido a medias.
- **mala**: objetivo no cumplido o estado incorrecto.

## 6. Límites
Un solo modelo (el del CLI local), pocas repeticiones, verificador propio. No mide Gemini ni otros clientes.

## 7. Actualización del 26-nov-2026 (flujo de ejecución de `25e7fa7`)
Los prompts y el SKILL de la primera ronda (25-nov) apuntaban al flujo anterior de `work_orders.*` (30 funciones: avance con respuestas y firma, `ConsumeFromOrder`, `ReturnLeftovers`). Desde entonces molu pasó a 33 funciones con el modelo de ejecución de Seam (spec 012 §8): responsable por bloque al crear, confirmación de cabecera, `ExecuteBlock`, `user_id` en el avance, y consumo con `RecordOrderConsumable` y `SettleOrderConsumable`. Cambios en este spec:
- **G1 y G2** se reescribieron para ese flujo (ver §3); se quitó la firma de G1. G2 ahora exige llevar la orden al estado final porque la liquidación solo existe con la orden terminada. L1, L2, V1 y V2 no cambian de texto; V2 ahora requiere además confirmar la cabecera para poder avanzar (más ambigüedad, a propósito).
- **SKILL** `work-orders-seam` reescrito (ejecución en 4 pasos, quién actúa, consumibles, reglas de error nuevas).
- **Verificadores** de `run.py`: G1 comprueba orden terminal, activos, cabecera confirmada con `recepcion_confirmada`, bloques completados y respuestas; G2 comprueba orden terminal, liquidación del filtro (usado 1, devuelto 3) y stock final (central 9, móvil 0). Validados con una corrida "oráculo" contra un xolu aparte: 0 sin hacer nada, 0.5 con avance parcial, 1.0 con la solución completa.
- **Reinicio de la demo** (`.dev-xolu/demo-reset.sh`, local y fuera de git): la orden de tablero se crea ahora con `block_responsibles`.
- El **informe** de la primera ronda se conserva como histórico (ver su cabecera): sus números no son comparables con los de la segunda ronda.
- **Segunda ronda hecha (26-nov-2026)**: 36 corridas en un stack aislado (`BENCH_XOLU_URL`, `BENCH_HUB_URL` y `BENCH_RESET` en `run.py`; `.dev-xolu/bench-reset.sh`, local). Resultados en `informe-ronda2.md` y `results/ronda2.jsonl`: perfiles 2 y 3 resuelven G1 y G2 perfectas (4/4), el perfil pelado no puede ejecutar el flujo (G1/G2 regular, V1 mala), y el SKILL no mejora la calidad y cuesta un 59 % más.
- **Tercera ronda hecha (26-nov-2026)**: perfil 4 (system prompt y preámbulo de Horacio + `molu_help`, sin SKILL; spec 016), 12 corridas. Resultados en `informe-ronda3.md` y `results/ronda3.jsonl`: calidad equivalente a los perfiles 2 y 3, y **0 llamadas a `molu_help` en las 12 corridas**: no demuestra que la ayuda sirva.
- **Cuarta ronda (27-nov-2026)**: los 4 perfiles con Claude Sonnet 5.5 y Haiku 5.5 (96 corridas; el modelo por defecto de las rondas anteriores era Opus 5.5). Selector `BENCH_CLAUDE_MODEL` en `run.py`; cada fila guarda el modelo. Resultados y comparación con todos los agentes (incluido Gemini de Gastón) en `informe-final.md`: Sonnet 11/12 en el perfil 2 (más rápido y a la mitad del costo de Opus), Haiku 10/12 con SKILL a ~0.005 USD por corrida, `molu_help` sin una sola llamada; V2 premia a quien confirma la cabecera sin responder el dato obligatorio.
- Los datos de todas las rondas están en `planilla-benchmark.xlsx` (se regenera con `make_planilla.py`, necesita `openpyxl`).
- Pendiente: ajustar la rúbrica de V2 (hoy una pregunta por un dato obligatorio cuenta como fallo parcial y adivinar cuenta como acierto), probar la ayuda con una superficie reducida de herramientas, tareas más ambiguas, Gemini en el perfil 4 y con más repeticiones.

## 8. Datos conservados
`results/*.jsonl` (una fila por corrida: calidad, tiempos, llamadas, errores, tokens, costo) se conservan de todas las rondas: bastan para rehacer cualquier tabla. Las transcripciones completas (`results/raw/`) solo ocupan espacio y sirven para auditar una corrida: se conservan únicamente las de la ronda más reciente de cada tipo de modelo y se borran las anteriores.
