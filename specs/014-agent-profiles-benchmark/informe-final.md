# Informe final — Benchmark de agentes sobre molu (spec 014)

Fecha: 27-nov-2026. Reúne todos los agentes probados con el mismo diseño: 6 tareas (2 de lectura, 2 guiadas, 2 vagas) que molu debe resolver por MCP, verificadas sobre el estado final de xolu. Datos en `results/*.jsonl` y en `planilla-benchmark.xlsx` (se regenera con `make_planilla.py`). Todas las cifras de este informe son del flujo de ejecución actual de `work_orders.*` (33 funciones, ronda 2 en adelante); la primera ronda (25-nov) usó un flujo anterior y no se mezcla (`informe.md`).

## 1. Qué se probó

| Agente | Quién / cómo | Perfiles | Repeticiones | Corridas |
|---|---|---|---|---|
| Claude Opus 5.5 (modelo por defecto del CLI) | Fabricio, CLI `claude -p` | 1, 2, 3 (ronda 2) y 4 (ronda 3) | 2 | 48 |
| Claude Sonnet 5.5 | Fabricio, mismo CLI con `--model sonnet` (ronda 4) | 1, 2, 3, 4 | 2 | 48 |
| Claude Haiku 5.5 | Fabricio, mismo CLI con `--model haiku` (ronda 4) | 1, 2, 3, 4 | 2 | 48 |
| Gemini 3.5 flash-lite | Gastón, runner MCP propio en Python (`gemini_runner.py`) | 1, 2, 3 | 1 | 18 |
| Gemini 3 flash (preview) | Gastón, mismo runner; se cortó por la cuota gratuita (20 solicitudes al día) | 1 | 1 | 2 de 36 |

**Perfiles:** 1 = agente pelado (solo los 13 primitivos de xolu, sin Hub); 2 = molu con las funciones de dominio, sin contexto extra; 3 = como el 2 con system prompt y el SKILL `work-orders-seam`; 4 = como el 2 con `molu_help`, el system prompt y el preámbulo de Horacio, sin SKILL (el perfil 4 no se probó con Gemini). Se probó además un modelo local de 3 B (llama3.2 con Ollama, solo CPU): no resolvió ninguna tarea (0/12) y esos datos no se conservaron.

**Rúbrica (automática):** perfecta = objetivo cumplido, sin preguntas y con hasta 2 errores de herramienta; buena = igual con más de 2 errores; aceptable = cumplido pero el agente preguntó; regular = a medias; mala = no cumplido. En las tablas, "Perfecta/buena" las suma.

## 2. Resultados por agente y perfil

| Agente | Perfil | Corridas | Perfecta/buena | Aceptable | Regular | Mala | % buena | Tiempo (s) | Llamadas | Errores | USD/corrida | USD total | USD por tarea lograda |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Claude Opus 5.5 | 1 Pelado | 12 | 5 | 0 | 5 | 2 | 42% | 22.7 | 10.6 | 1.0 | 0.150 | 1.79 | 0.359 |
| Claude Opus 5.5 | 2 Seam+xolu | 12 | 9 | 1 | 2 | 0 | 75% | 14.8 | 6.4 | 0.0 | 0.082 | 0.99 | 0.110 |
| Claude Opus 5.5 | 3 +SKILL | 12 | 10 | 0 | 2 | 0 | 83% | 16.5 | 7.2 | 0.1 | 0.131 | 1.57 | 0.157 |
| Claude Opus 5.5 | 4 +molu_help | 12 | 9 | 1 | 2 | 0 | 75% | 15.5 | 6.5 | 0.1 | 0.097 | 1.17 | 0.130 |
| Claude Sonnet 5.5 | 1 Pelado | 12 | 4 | 0 | 6 | 2 | 33% | 17.4 | 10.4 | 1.1 | 0.079 | 0.94 | 0.236 |
| Claude Sonnet 5.5 | 2 Seam+xolu | 12 | 11 | 1 | 0 | 0 | 92% | 11.2 | 6.1 | 0.0 | 0.045 | 0.54 | 0.049 |
| Claude Sonnet 5.5 | 3 +SKILL | 12 | 10 | 1 | 1 | 0 | 83% | 12.0 | 6.9 | 0.0 | 0.065 | 0.79 | 0.079 |
| Claude Sonnet 5.5 | 4 +molu_help | 12 | 12 | 0 | 0 | 0 | 100% | 11.5 | 6.3 | 0.2 | 0.047 | 0.56 | 0.047 |
| Claude Haiku 5.5 | 1 Pelado | 12 | 4 | 0 | 5 | 3 | 33% | 24.4 | 11.6 | 2.1 | 0.006 | 0.07 | 0.018 |
| Claude Haiku 5.5 | 2 Seam+xolu | 12 | 8 | 0 | 4 | 0 | 67% | 16.4 | 6.4 | 0.2 | 0.004 | 0.05 | 0.006 |
| Claude Haiku 5.5 | 3 +SKILL | 12 | 10 | 0 | 1 | 1 | 83% | 16.4 | 8.0 | 0.2 | 0.005 | 0.06 | 0.006 |
| Claude Haiku 5.5 | 4 +molu_help | 12 | 8 | 0 | 3 | 1 | 67% | 17.2 | 6.9 | 0.2 | 0.005 | 0.06 | 0.007 |
| Gemini 3.5 flash-lite | 1 Pelado | 6 | 2 | 0 | 2 | 2 | 33% | 86.2 | 27.3 | 18.8 | — | — | — |
| Gemini 3.5 flash-lite | 2 Seam+xolu | 6 | 6 | 0 | 0 | 0 | 100% | 25.0 | 7.2 | 0.2 | — | — | — |
| Gemini 3.5 flash-lite | 3 +SKILL | 6 | 5 | 0 | 1 | 0 | 83% | 30.9 | 8.2 | 0.2 | — | — | — |
| Gemini 3 flash (preview) | 1 Pelado | 2 | 2 | 0 | 0 | 0 | 100% | 29.8 | 5.5 | 0.0 | 0.020 | 0.04 | 0.020 |

El costo en dólares solo existe donde la herramienta lo informa (Claude y una muestra mínima de Gemini); "USD por tarea lograda" es el costo total dividido por las corridas perfectas o buenas.

## 3. Resultados por tarea

Calidad por prompt (calidad de cada repetición: P=perfecta/buena, A=aceptable, R=regular, M=mala)

Perfil 1 Pelado
| Prompt | Claude Opus 5.5 | Claude Sonnet 5.5 | Claude Haiku 5.5 | Gemini 3.5 flash-lite | Gemini 3 flash (preview) |
|---|---|---|---|---|---|
| L1 | PP | PP | PP | P | P |
| L2 | PP | PP | PP | P | P |
| G1 | RR | RR | RR | R | — |
| G2 | RR | RR | RR | R | — |
| V1 | MM | MM | MM | M | — |
| V2 | RP | RR | MR | M | — |

Perfil 2 Seam+xolu
| Prompt | Claude Opus 5.5 | Claude Sonnet 5.5 | Claude Haiku 5.5 | Gemini 3.5 flash-lite | Gemini 3 flash (preview) |
|---|---|---|---|---|---|
| L1 | PP | PP | PP | P | — |
| L2 | PA | PA | PP | P | — |
| G1 | PP | PP | RP | P | — |
| G2 | PP | PP | RP | P | — |
| V1 | PP | PP | PP | P | — |
| V2 | RR | PP | RR | P | — |

Perfil 4 +molu_help
| Prompt | Claude Opus 5.5 | Claude Sonnet 5.5 | Claude Haiku 5.5 | Gemini 3.5 flash-lite | Gemini 3 flash (preview) |
|---|---|---|---|---|---|
| L1 | PP | PP | PP | — | — |
| L2 | PA | PP | PP | — | — |
| G1 | PP | PP | PR | — | — |
| G2 | PP | PP | PP | — | — |
| V1 | PP | PP | PM | — | — |
| V2 | RR | PP | RR | — | — |

## 4. Lo que muestran los datos

1. **Lo que más pesa es tener funciones de dominio, no el modelo.** Con el agente pelado (perfil 1) ningún modelo resuelve las tareas que exigen la lógica del sistema: G1, G2 y V1 salen regular o mala en los cuatro agentes (Opus, Sonnet, Haiku y Gemini), con 4 a 5 corridas buenas de 12 (33 a 42 %), casi todas de lectura. Con las funciones de dominio (perfil 2) los mismos modelos suben a 67 a 100 %.
2. **En las tareas guiadas, un modelo más simple rinde casi igual y cuesta mucho menos.** Sin contar V2 (ver punto 4), Opus y Sonnet logran 9 de 10 corridas en el perfil 2 y Haiku 8 de 10. Sonnet es además más rápido (11.2 s contra 14.8 s) y cuesta casi la mitad (0.045 USD por corrida contra 0.082). Haiku cuesta unas 20 veces menos que Opus (0.004 USD por corrida). Por tarea lograda: Haiku 0.006 USD, Sonnet 0.047 USD, Opus 0.110 USD.
3. **Haiku es el que más se equivoca, y de forma reconocible.** En G1 olvidó responder el ítem obligatorio "recepción confirmada" y confirmó la cabecera igual (2 de las 4 corridas de los perfiles 2 y 4; la orden quedó terminada con la recepción sin registrar), en G2 se detuvo a pedir un dato a mitad de camino y en V1 dos veces dijo que no podía reponer el stock. El SKILL lo corrige en gran parte: con SKILL (perfil 3) Haiku sube de 8 a 10 corridas buenas de 12, igual que Opus y Sonnet. Con Sonnet y Opus el SKILL no mejora la calidad y cuesta de un 44 a un 60 % más.
4. **V2 mide el criterio del agente, y la rúbrica lo puntúa al revés.** La orden del tablero necesita para avanzar un dato obligatorio ("recepción confirmada") que solo puede dar una persona. Opus y Haiku reclaman la orden con el usuario correcto y se detienen a preguntar: la rúbrica los marca como regular. Sonnet (perfecto en sus 4 corridas) y Gemini 3.5 flash-lite avanzaron la orden: en las transcripciones revisadas (2 de Sonnet y 1 de Gemini) **confirmaron la cabecera sin enviar ningún valor**. La rúbrica los marca como perfectos, pero lo que hicieron fue dejar "recepción confirmada" sin responder. Es un hallazgo sobre molu: `ConfirmExecutionHeader` acepta omitir un ítem booleano obligatorio, que queda en falso, igual que el formulario de Seam (una casilla sin marcar es válida). Para un agente conviene decidir si exigir el valor explícito. Hasta entonces las cifras de V2 no sirven para ordenar modelos.
5. **`molu_help` no se usó ni una vez.** En el perfil 4, Opus, Sonnet y Haiku hicieron 0 llamadas a la ayuda en 36 corridas. Sus resultados son equivalentes a los del perfil 2 (Sonnet 12/12 contra 11/12, Haiku 8/12 contra 8/12, Opus 9/12 contra 9/12): lo que cambia es el system prompt y el preámbulo, no la ayuda. Con modelos más simples tampoco apareció la necesidad de consultarla.
6. **Gemini 3.5 flash-lite** acertó las 6 corridas del perfil 2 y 5 de las 6 con SKILL, pero con una sola repetición, sin perfil 4 y sin dato de costo; como pelado fue el más lento (86 s y 18.8 errores por corrida). Gemini 3 flash (preview) tiene 2 corridas, de lectura, que no permiten concluir nada.

## 5. Límites
- Pocas repeticiones (2 por celda en Claude, 1 en Gemini): una corrida cambia un 8 a 17 % de la tasa; las diferencias de uno o dos puntos son ruido. La variación del costo entre pasadas del mismo perfil fue de ±25 %.
- Verificador y rúbrica propios; V2 no ordena modelos (punto 4).
- Los tiempos incluyen la carga de la máquina de ese momento; los de Gemini vienen de otro runner y de cuota gratuita.
- No hay datos de Gemini en el perfil 4 ni de costos de Gemini; el modelo local de 3 B quedó fuera por no resolver ninguna tarea.

## 6. Qué haría con esto
1. **Para uso general: Sonnet 5.5 con funciones de dominio** (perfil 2): mismo resultado que Opus en las guiadas, más rápido y a la mitad del costo.
2. **Para volumen o costo mínimo: Haiku 5.5 con SKILL** (perfil 3): 10 de 12 a unos 0.005 USD por corrida; hay que vigilar que no omita datos obligatorios.
3. **Decidir si `ConfirmExecutionHeader` exige respuesta explícita a los ítems booleanos obligatorios**, y ajustar la rúbrica de V2 para separar "preguntó lo que no podía saber" de "adivinó".
4. Si se quiere ver si la ayuda aporta: probar con una superficie reducida de herramientas (solo las de la tarea), que es donde un modelo pequeño se queda sin pistas.
