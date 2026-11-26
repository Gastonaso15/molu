# Informe — Benchmark de perfiles de agente, segunda ronda (spec 014 §7)

Fecha: 26-nov-2026. 36 corridas (3 perfiles × 6 prompts × 2 repeticiones), mismo diseño y mismo modelo que la primera ronda (`informe.md`, 25-nov). Cambió el objeto medido: `work_orders.*` ahora son 33 funciones con el modelo de ejecución de Seam (responsable por bloque, confirmación de cabecera, `ExecuteBlock`, `user_id`, consumo con `RecordOrderConsumable` y liquidación con `SettleOrderConsumable`). El SKILL, los prompts G1/G2 y los verificadores se actualizaron para ese flujo (§7 de la spec). Corrió en un **stack aislado** (xolu 9898, Hub 8391, publicador 8392), con el estado reiniciado antes de cada corrida que muta; no se tocó el xolu de desarrollo. Datos en `results/ronda2.jsonl` (resumen por corrida). Las transcripciones completas de esta ronda se eliminaron al hacer las siguientes (solo se conservan las más recientes).

## 1. Resumen por perfil

| Perfil | Perfecta/buena | Aceptable | Regular | Mala | Tiempo medio (s) | Llamadas medias | Errores medios | Costo medio (USD) |
|---|---|---|---|---|---|---|---|---|
| 1. Agente pelado (solo xolu, 13 primitivos) | 5/12 | 0 | 5 | 2 | 22.7 | 10.6 | 1.0 | 0.15 |
| 2. Seam + xolu (46 herramientas) | 9/12 | 1 | 2 | 0 | 14.8 | 6.4 | 0.0 | 0.08 |
| 3. Seam + xolu + system prompt + SKILL | 10/12 | 0 | 2 | 0 | 16.5 | 7.2 | 0.1 | 0.13 |

Costo total de la ronda: 4.35 USD (perfil 2: 0.99; perfil 3: 1.57).

## 2. Por prompt (calidad de las dos repeticiones)

| Prompt | Perfil 1 | Perfil 2 | Perfil 3 |
|---|---|---|---|
| L1 plantillas | perfecta, perfecta | perfecta, perfecta | perfecta, perfecta |
| L2 stock bajo | perfecta, perfecta | perfecta, aceptable | perfecta, perfecta |
| G1 traslado completo | regular, regular | perfecta, perfecta | perfecta, perfecta |
| G2 consumibles completo | regular, regular | perfecta, perfecta | perfecta, perfecta |
| V1 reponer stock | mala, mala | perfecta, perfecta | perfecta, perfecta |
| V2 orden del tablero | regular, buena | regular, regular | regular, regular |

## 3. Qué muestran los datos

1. **Con las funciones de dominio, las tareas guiadas salen bien aun con el flujo más largo.** Los perfiles 2 y 3 resuelven G1 y G2 de forma perfecta en las 4 repeticiones, sin errores de herramienta, deduciendo solos el usuario que actúa y el responsable de cada bloque. G2 cuesta más que en la primera ronda (31.9 s y 17.5 llamadas frente a 23.0 s y 11.5 en el perfil 2) porque el flujo tiene más pasos: cabecera, bloques, carga, avance y liquidación.
2. **El agente pelado no puede resolver el flujo nuevo.** G1 y G2 quedan en regular en las 4 repeticiones: con las primitivas genéricas solo puede guardar un registro de orden, sin la máquina de estados, los bloques ni el almacén móvil, y lo dice ("no pude llevar la orden al estado final"). V1 queda mala en las 2: no ve alertas de stock bajo (no interpreta las existencias, guardadas en centésimas, ni cruza depósitos). Es el mismo patrón de la primera ronda, ahora más marcado.
3. **El SKILL no mejora la calidad y encarece.** El perfil 3 usó el SKILL en 10 de 12 corridas (todas menos L2) y no resolvió nada más que el perfil 2: el 10/12 contra 9/12 sale de una repetición de L2 en la que el perfil 2 hizo una pregunta. A cambio fue 11 % más lento (16.5 s contra 14.8 s) y 59 % más caro (1.57 USD contra 0.99 USD en total), por el costo de cargar el SKILL. Efecto techo, como en la primera ronda.
4. **V2 ya no es una tarea que se pueda resolver sin preguntar.** Los perfiles 2 y 3 reclaman la orden con el usuario correcto (el responsable de la Cuadrilla Traslados) y se detienen antes de avanzarla porque falta el dato obligatorio "recepción confirmada", que ninguna función permite deducir. Es la conducta correcta, pero la rúbrica la puntúa como regular (objetivo a medias). El único "buena" de V2 es del perfil pelado, que lo resolvió sin preguntar (supuso el valor).
5. **Las 33 funciones no confundieron al agente.** El perfil 2 tuvo 0 errores de herramienta en 12 corridas, con 46 herramientas visibles.

## 4. Límites
Un solo modelo (el del CLI local), solo 2 repeticiones por celda y verificador propio; no mide Gemini ni otros clientes. La rúbrica trata una pregunta legítima (un dato obligatorio que no se puede deducir) como un fallo parcial: V2 mide hoy más la conducta ante un dato faltante que la capacidad de resolver. Costos y tiempos dependen de la caché y de la carga del momento.

## 5. Siguiente
- Ajustar la rúbrica de V2 (o añadir una variante con el dato en el prompt) para separar "no sabe" de "pide el dato que falta".
- Perfil 4 con la idea de Horacio (system prompt fijo + preámbulo corto + función `molu_help` con textos), comparado contra el perfil 3.
- Repetir con tareas más ambiguas y con el cliente de Gastón (Gemini).
