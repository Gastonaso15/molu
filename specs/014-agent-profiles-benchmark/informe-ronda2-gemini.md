# Informe — Benchmark de perfiles de agente con Gemini (spec 014 §7)

Fecha: 26-nov-2026. 18 corridas (3 perfiles × 6 prompts × 1 repetición) ejecutadas con el runner MCP propio en Python (`gemini_runner.py`) contra `gemini-3.5-flash-lite` (único modelo de la API key en Free Tier que completó las 18 corridas sin agotar cuota diaria; `gemini-3-flash-preview` alcanzó 2/36 antes de agotar el tope de 20 req/día en `results/ronda2-gemini-flash.jsonl`). Mismo diseño de prompts, SKILL, verificadores y stack aislado (`xolu` 0.31.0 en `:9898`, `hub` en `:8391`, `publisher-orders` en `:8392`, reiniciado antes de cada corrida que muta) que la segunda ronda con Claude (`informe-ronda2.md`). Datos crudos: `results/ronda2-gemini-35-flash-lite.jsonl` y `results/raw/ronda2-gemini-35-flash-lite-*.jsonl`.

## 1. Resumen por perfil (`gemini-3.5-flash-lite`, 1 repetición)

| Perfil | Perfecta | Regular | Mala | Tiempo medio (s) | API medio (s) | Turnos medios | Llamadas medias | Errores medios | Tokens entrada (totales / cacheados) | Tokens salida | Usó SKILL |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1. Agente pelado (solo xolu, 13 primitivos) | 2/6 | 2 | 2 | 86.2 | 58.8 | 27.0 | 27.3 | 18.8 | 1,535,689 / 1,011,087 | 7,144 | 0/6 |
| 2. Seam + xolu (46 herramientas) | 6/6 | 0 | 0 | 25.0 | 14.7 | 6.7 | 7.2 | 0.2 | 594,329 / 416,943 | 2,736 | 0/6 |
| 3. Seam + xolu + system prompt + SKILL | 5/6 | 1 | 0 | 30.9 | 13.8 | 8.2 | 8.2 | 0.2 | 778,453 / 555,018 | 2,690 | 6/6 |

*(Nota: `usd` figura en `0.0` en el JSONL porque el modelo `gemini-3.5-flash-lite` no está en el tarifario estático de `gemini_runner.py`.)*

## 2. Detalle por prompt (1 repetición)

| Prompt | Perfil 1 (pelado) | Perfil 2 (`work_orders.*`) | Perfil 3 (`+ SKILL`) |
|---|---|---|---|
| L1 plantillas | perfecta (`6.3s`, 2 calls) | perfecta (`4.2s`, 1 call) | perfecta (`3.9s`, 2 calls) |
| L2 stock bajo | perfecta* (`22.5s`, 5 calls) | perfecta (`2.4s`, 1 call) | perfecta (`5.4s`, 2 calls) |
| G1 traslado completo | regular (`50.5s`, 17 calls, 5 err) | perfecta (`32.9s`, 11 calls, 0 err) | perfecta (`47.9s`, 11 calls, 0 err) |
| G2 consumibles completo | regular (`208.7s`, 65 calls, 53 err) | perfecta (`56.2s`, 19 calls, 1 err) | perfecta (`55.9s`, 20 calls, 0 err) |
| V1 reponer stock | mala (`29.8s`, 15 calls, 0 err) | perfecta (`6.3s`, 4 calls, 0 err) | perfecta (`11.8s`, 7 calls, 1 err) |
| V2 orden del tablero | mala (`199.5s`, 60 calls, 55 err) | perfecta (`47.8s`, 7 calls, 0 err) | regular (`60.2s`, 7 calls, 0 err) |

*\*En L2 del perfil 1, el verificador automático puntúa 1 porque aparecen "Aceite" y "Central" en el texto, pero la respuesta del agente en realidad concluye que no hay stock bajo (leyó `300` centésimas contra mínimo `5`).*

## 3. Qué muestran los datos

1. **La brecha entre herramientas de dominio (perfil 2) y agente pelado (perfil 1) es aún más marcada en un modelo ligero.** Con `gemini-3.5-flash-lite`, el perfil 2 resuelve **6/6** en `25.0s` de media (`14.7s` de API) y `7.2` llamadas, mientras que el perfil 1 cae en bucles de hasta `60` turnos y `53–55` errores en G2 y V2 (`86.2s` de media total, `2.6×` más tokens de entrada) y falla en V1 (`mala`) por no interpretar las centésimas ni los depósitos.
2. **Las tareas guiadas complejas (G1 y G2) salen perfectas con `work_orders.*` incluso en `flash-lite`.** Tanto el perfil 2 como el 3 completan todo el flujo nuevo (cabecera, bloques con responsables, almacén móvil, avance hasta estado terminal y liquidación de consumibles) con 0–1 error de herramienta.
3. **El SKILL no aporta mejora sobre el perfil 2 y suma ~31 % más tokens de entrada.** El perfil 3 cargó `Skill("work-orders-seam")` en 6/6 corridas (`778k` tokens de entrada vs `594k` del perfil 2), y fue ligeramente más lento en tiempo total (`30.9s` vs `25.0s`). En `V2`, el perfil 3 reclamó bien con la cuadrilla correcta (`responsable=2`), pero terminó en `finish_reason: MALFORMED_FUNCTION_CALL` antes de responder (`regular`), mientras que el perfil 2 completó el avance suponiendo el valor de cabecera y reasignando a la Cuadrilla Mantenimiento (`responsable=1`).
4. **Muestra parcial previa con `gemini-3-flash-preview`:** en `results/ronda2-gemini-flash.jsonl` (2/36 corridas antes del tope Free Tier de 20 req/día), el perfil 1 en `L1` y `L2` obtuvo `perfecta` en `5.5s` (3 turnos) y `54.1s` (10 turnos).

## 4. Límites

- **1 repetición por celda** en `gemini-3.5-flash-lite` (muestra indicativa, no estadística).
- Modelo `flash-lite` sin tokens de razonamiento (`thinking_tokens: 0`) seleccionado por disponibilidad de cuota en Free Tier; los modelos `pro` y `flash` completos devolvieron `429` o `503`/límite diario de 20 req/día en esta API key.
- Mismo límite de rúbrica señalado en `informe-ronda2.md` para `L2` (chequeo de texto en perfil 1) y `V2`.

## 5. Reproducir

```bash
export GEMINI_API_KEY="..."   # o guardarla en .env (ignorado por git)
python3 specs/014-agent-profiles-benchmark/run.py --runner gemini --model gemini-3.5-flash-lite --reps 1 --tag ronda2-gemini-35-flash-lite
```
