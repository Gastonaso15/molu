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

Control común: mismo modelo, carpeta de trabajo vacía fuera del repo, sin herramientas de archivos,
sin memoria ni settings del usuario, `demo-reset.sh` antes de cada corrida que muta estado.

## 3. Prompts (los mismos en los tres perfiles)
| Id | Tipo | Resumen |
|---|---|---|
| L1 | lectura | plantillas existentes y estados de la de traslado |
| L2 | lectura | consumibles con stock bajo y dónde |
| G1 | guiada | crear orden de traslado, avanzarla al estado final con respuestas y firma |
| G2 | guiada | flujo de consumibles (orden, entrada, transferencia, consumo, devolución) |
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
