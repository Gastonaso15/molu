# Informe — Benchmark de perfiles de agente (spec 014)

Fecha: 25-nov-2026. 36 corridas (3 perfiles × 6 prompts × 2 repeticiones). Mismo modelo (el del CLI local), agente de cero en carpeta vacía, sin herramientas de archivos, sin acceso al código de Seam. Estado de xolu reiniciado antes de cada corrida que muta. Calidad calculada sobre el estado final de xolu (rúbrica en `spec.md`). Datos crudos: `results/main.jsonl` y `results/raw/`.

## 1. Resumen por perfil

| Perfil | Perfecta/buena | Regular | Mala | Tiempo medio (s) | Llamadas medias | Costo medio (USD) |
|---|---|---|---|---|---|---|
| 1. Agente pelado (solo xolu, 13 primitivos) | 5/12 | 5 | 2 | 24.3 | 10.9 | 0.15 |
| 2. Seam + xolu (43 herramientas) | 12/12 | 0 | 0 | 13.8 | 5.2 | 0.08 |
| 3. Seam + xolu + system prompt + SKILL | 12/12 | 0 | 0 | 13.6 | 5.8 | 0.09 |

## 2. Detalle por prompt (repetición 1 / repetición 2)

| Prompt | Perfil | Calidad | Tiempo (s) | Llamadas | Errores | Usó SKILL |
|---|---|---|---|---|---|---|
| L1 (lectura) | 1 | perfecta / perfecta | 10.9 / 10.7 | 3 / 3 | 0 / 0 | no / no |
| L1 (lectura) | 2 | perfecta / perfecta | 7.9 / 7.7 | 1 / 1 | 0 / 0 | no / no |
| L1 (lectura) | 3 | perfecta / perfecta | 8.9 / 9.2 | 2 / 2 | 0 / 0 | sí / sí |
| L2 (lectura) | 1 | perfecta / perfecta | 11.4 / 11.2 | 4 / 4 | 0 / 0 | no / no |
| L2 (lectura) | 2 | perfecta / perfecta | 6.1 / 5.3 | 1 / 1 | 0 / 0 | no / no |
| L2 (lectura) | 3 | perfecta / perfecta | 6.0 / 5.6 | 1 / 1 | 0 / 0 | no / no |
| G1 (guiada) | 1 | regular / regular | 27.6 / 25.8 | 11 / 11 | 1 / 1 | no / no |
| G1 (guiada) | 2 | perfecta / perfecta | 17.0 / 17.1 | 7 / 7 | 0 / 0 | no / no |
| G1 (guiada) | 3 | perfecta / perfecta | 17.7 / 16.6 | 8 / 8 | 0 / 0 | sí / sí |
| G2 (guiada) | 1 | regular / regular | 31.4 / 35.7 | 18 / 19 | 1 / 2 | no / no |
| G2 (guiada) | 2 | perfecta / perfecta | 22.9 / 23.2 | 11 / 12 | 0 / 0 | no / no |
| G2 (guiada) | 3 | perfecta / perfecta | 26.0 / 23.2 | 13 / 12 | 0 / 0 | sí / sí |
| V1 (vaga) | 1 | mala / mala | 13.8 / 18.2 | 7 / 8 | 0 / 0 | no / no |
| V1 (vaga) | 2 | perfecta / perfecta | 13.9 / 16.1 | 5 / 5 | 0 / 0 | no / no |
| V1 (vaga) | 3 | perfecta / perfecta | 11.3 / 11.0 | 5 / 5 | 0 / 0 | sí / sí |
| V2 (vaga) | 1 | buena / regular | 47.0 / 47.4 | 22 / 21 | 4 / 3 | no / no |
| V2 (vaga) | 2 | perfecta / perfecta | 13.4 / 14.4 | 6 / 6 | 0 / 0 | no / no |
| V2 (vaga) | 3 | perfecta / perfecta | 13.7 / 14.2 | 6 / 6 | 0 / 0 | sí / sí |

## 3. Lectura de los resultados

1. **Funciones de dominio (perfil 2) frente al agente pelado (perfil 1):** es la diferencia grande. El perfil 2 completó 12/12 corridas de forma perfecta; el perfil 1 resolvió bien las 4 lecturas simples y una de las dos corridas de V2 (5/12). En las tareas guiadas de escritura (G1, G2) se quedó a medias en las 4 corridas, y en V1 (stock bajo) falló en las 2.
2. **Por qué falla el perfil 1:** (a) las primitivas genéricas no ejecutan la lógica del dominio: creó el registro de la orden con `create` pero no pudo avanzarla ni mover stock de forma fiable, y lo dijo y se detuvo; (b) **malinterpretó las unidades**: xolu guarda el stock en centésimas (300 = 3,00) y concluyó que no había stock bajo (300 contra mínimo 5), sin hacer ningún cambio. Es justo la semántica de Seam que las funciones `work_orders.*` ya traducen.
3. **Tiempo:** el perfil 1 no es más rápido en lo que termina; en V2 tardó ~47 s con 21–22 llamadas frente a ~14 s y 6 llamadas del perfil 2. En las 4 tareas de escritura el perfil 2 fue entre 1,1× (V1) y 3,4× (V2) más rápido.
4. **SKILL y system prompt (perfil 3) frente al perfil 2:** no hubo mejora medible. Ambos 12/12 perfectos. El perfil 3 usó el SKILL en 10 de 12 corridas, pero con tiempos y llamadas iguales o algo mayores (G2: 24,6 s frente a 23,1 s; G1: 17,2 s frente a 17,1 s). Efecto techo: con estas tareas, las descripciones de las herramientas ya bastan.
5. **Sobre el código fuente de Seam:** ningún perfil lo tuvo y el perfil 2 resolvió todo. Para operar, el código no es necesario; el conocimiento semántico necesario ya viaja en las descripciones y contratos de las funciones publicadas.

## 4. Límites (no sobreinterpretar)

- Un solo modelo y un solo cliente (Claude Code en modo `-p`). **No se midió Gemini ni el cliente de Gastón**, así que esto no explica sus minutos de demora; sí descarta a molu y a la falta de código como causa en este entorno.
- 2 repeticiones por celda: los tiempos son indicativos, no estadísticos. El primer prompt de cada bloque puede incluir efectos de caché.
- Los prompts son pocos y con un techo bajo: el perfil 3 podría mostrar ventaja con tareas más ambiguas, largas o con más entidades, que no se probaron.
- La calidad la calcula un verificador propio sobre el estado final de xolu. El del perfil 1 en G1 (`estado=` vacío) sale de que la orden creada con la primitiva `create` no tiene el estado del ciclo de plantilla; es coherente con la respuesta del propio agente, pero la nota "regular" es una decisión de la rúbrica.
- El perfil 3 incluye también las skills integradas de Claude Code en la lista; solo `work-orders-seam` se relaciona con la tarea.

## 5. Reproducir

```
./.dev-xolu/demo-reset.sh   # estado limpio de la demo (borra el xolu local 9595)
BENCH_WORK=<carpeta> python3 specs/014-agent-profiles-benchmark/run.py --reps 2 --tag main
```
