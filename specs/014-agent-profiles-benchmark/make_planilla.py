#!/usr/bin/env python3
"""Genera planilla-benchmark.xlsx con todos los resultados del benchmark (spec 014) a partir de results/*.jsonl.
Necesita openpyxl (pip install openpyxl). Uso: python3 make_planilla.py"""
import json, os, statistics as st, collections, importlib.util
from openpyxl import Workbook
from openpyxl.styles import Font, PatternFill, Alignment, Border, Side
from openpyxl.utils import get_column_letter

HERE = os.path.dirname(os.path.abspath(__file__)); R = os.path.join(HERE, "results")
spec = importlib.util.spec_from_file_location("run014", os.path.join(HERE, "run.py")); run = importlib.util.module_from_spec(spec); spec.loader.exec_module(run)

R1 = "R1 · 25-nov · flujo anterior (30 funciones)"
R2 = "R2 · 26-nov · flujo de ejecución (33 funciones)"
R3 = "R3 · 26-nov · perfil 4 con molu_help"
R4 = "R4 · 27-nov · modelos Claude más simples (Sonnet y Haiku)"
SOURCES = [  # archivo, ronda, modelo (si el archivo no lo trae)
    ("main.jsonl", R1, "Claude (CLI, modelo por defecto)"),
    ("ronda2.jsonl", R2, "claude-opus-5-5 (CLI por defecto)"),
    ("ronda3.jsonl", R3, "claude-opus-5-5 (CLI por defecto)"),
    ("ronda4-sonnet.jsonl", R4, None),
    ("ronda4-haiku.jsonl", R4, None),
    ("ronda2-gemini-35-flash-lite.jsonl", R2, None),
    ("ronda2-gemini-flash.jsonl", R2, None),
]
PERFIL = {1: "1. Pelado (13 primitivos)", 2: "2. Seam + xolu", 3: "3. + system prompt + SKILL", 4: "4. + system prompt + preámbulo + molu_help"}

def label_model(row, default):
    m = row.get("model") or default or "?"
    if m == "claude-opus-5-5": m = "claude-opus-5-5 (CLI por defecto)"
    return m

rows = []
for fn, ronda, default_model in SOURCES:
    p = os.path.join(R, fn)
    if not os.path.exists(p): continue
    for line in open(p):
        if not line.lstrip().startswith('{'): continue  # ignora texto ajeno pegado por error
        r = json.loads(line)
        rows.append(dict(ronda=ronda, modelo=label_model(r, default_model), perfil=r["profile"], prompt=r["prompt"], tipo=r.get("kind", ""), rep=r["rep"],
                         calidad=r["quality"], puntaje=r["score"], pregunto="sí" if r.get("asked") else "no", wall=r.get("wall_s"), api=r.get("api_s"),
                         turnos=r.get("turns"), llamadas=r.get("tool_calls"), errores=r.get("tool_errors"), usd=r.get("usd"),
                         skill="sí" if r.get("used_skill") else "no", help=r.get("help_calls", 0 if "help_calls" not in r else r["help_calls"]),
                         tok_in=r.get("input_tokens") or r.get("prompt_tokens"), tok_out=r.get("output_tokens"), detalle=r.get("detail", "")))
order = {R1: 1, R2: 2, R3: 3, R4: 4}
rows.sort(key=lambda x: (order[x["ronda"]], x["modelo"], x["perfil"], ["L1", "L2", "G1", "G2", "V1", "V2"].index(x["prompt"]), x["rep"]))

HEAD = PatternFill("solid", fgColor="1F3A5F"); HFONT = Font(bold=True, color="FFFFFF"); thin = Side(style="thin", color="D0D0D0")
FILL = {"perfecta": "C6EFCE", "buena": "E2F0D9", "aceptable": "FFF2CC", "regular": "FCE4D6", "mala": "F8CBAD"}

def header(ws, cols, widths):
    ws.append(cols)
    for i, c in enumerate(ws[1], 1):
        c.fill, c.font, c.alignment = HEAD, HFONT, Alignment(horizontal="center", vertical="center", wrap_text=True)
        ws.column_dimensions[get_column_letter(i)].width = widths[i - 1]
    ws.row_dimensions[1].height = 34; ws.freeze_panes = "A2"

wb = Workbook()
# ---- Resumen
ws = wb.active; ws.title = "Resumen"
cols = ["Ronda", "Modelo", "Perfil", "Corridas", "Perfecta+buena", "Aceptable", "Regular", "Mala", "% perfecta+buena", "Tiempo medio (s)", "Llamadas medias", "Errores medios", "Costo medio (USD)", "Costo total (USD)", "Llamadas a molu_help"]
header(ws, cols, [44, 32, 38, 9, 14, 11, 9, 7, 13, 13, 12, 11, 13, 13, 13])
groups = collections.OrderedDict()
for r in rows: groups.setdefault((r["ronda"], r["modelo"], r["perfil"]), []).append(r)
def mean(rs, k):
    v = [x[k] for x in rs if isinstance(x[k], (int, float))]; return round(st.mean(v), 2) if v else None
for (ronda, modelo, perfil), rs in groups.items():
    q = collections.Counter(x["calidad"] for x in rs); n = len(rs); good = q["perfecta"] + q["buena"]
    usd = [x["usd"] for x in rs if isinstance(x["usd"], (int, float)) and x["usd"] > 0]
    ws.append([ronda, modelo, PERFIL[perfil], n, good, q["aceptable"], q["regular"], q["mala"], good / n, mean(rs, "wall"), mean(rs, "llamadas"), mean(rs, "errores"),
               round(st.mean(usd), 3) if usd else None, round(sum(usd), 2) if usd else None, sum(x["help"] or 0 for x in rs)])
    ws.cell(ws.max_row, 9).number_format = "0%"
ws.auto_filter.ref = ws.dimensions
# ---- Corridas
wc = wb.create_sheet("Corridas")
cols = ["Ronda", "Modelo", "Perfil", "Perfil (nombre)", "Prompt", "Tipo", "Rep", "Calidad", "Puntaje", "Preguntó", "Tiempo (s)", "Tiempo API (s)", "Turnos", "Llamadas", "Errores", "Costo (USD)", "Usó SKILL", "Llamadas a molu_help", "Tokens entrada", "Tokens salida", "Detalle del verificador"]
header(wc, cols, [44, 32, 7, 36, 8, 9, 5, 11, 8, 9, 10, 10, 8, 9, 8, 10, 9, 11, 13, 12, 70])
for r in rows:
    wc.append([r["ronda"], r["modelo"], r["perfil"], PERFIL[r["perfil"]], r["prompt"], r["tipo"], r["rep"], r["calidad"], r["puntaje"], r["pregunto"], r["wall"], r["api"], r["turnos"], r["llamadas"], r["errores"], r["usd"], r["skill"], r["help"], r["tok_in"], r["tok_out"], r["detalle"]])
    wc.cell(wc.max_row, 8).fill = PatternFill("solid", fgColor=FILL.get(r["calidad"], "FFFFFF"))
wc.auto_filter.ref = wc.dimensions
# ---- Prompts
wp = wb.create_sheet("Prompts")
header(wp, ["Id", "Tipo", "Muta estado", "Texto (versión vigente desde R2)", "Qué comprueba el verificador"], [6, 10, 12, 100, 80])
CHECK = {"L1": "Menciona los 3 estados de la plantilla de traslado (Preparada, En traslado, Entregada).", "L2": "Menciona Aceite hidráulico y el Depósito Central.",
         "G1": "Orden de traslado terminal asignada a la cuadrilla 2, activos 19 y 20, cabecera confirmada con recepción confirmada, bloques completados y respuestas (punto y fecha).",
         "G2": "Orden de mantenimiento terminal de la cuadrilla 1, filtro liquidado (usado 1, devuelto 3), stock final central 9 y móvil 0.",
         "V1": "Sin alertas de stock bajo al final.", "V2": "Orden del tablero con responsable (1 punto) y avanzada un paso (1 punto completo)."}
for pid, (kind, mut, text) in run.PROMPTS.items(): wp.append([pid, kind, "sí" if mut else "no", text, CHECK[pid]])
for row in wp.iter_rows(min_row=2):
    for c in row: c.alignment = Alignment(wrap_text=True, vertical="top")
# ---- Notas
wn = wb.create_sheet("Notas")
wn.column_dimensions["A"].width = 150
for line in [
    "Benchmark de perfiles de agente sobre molu (spec 014). Datos generados por make_planilla.py a partir de results/*.jsonl.",
    "",
    "PERFILES",
    "1 Pelado: solo los 13 primitivos genéricos de xolu (sin Hub). 2 Seam + xolu: primitivos + funciones work_orders.*. 3: lo anterior + system prompt de priming + SKILL work-orders-seam.",
    "4: como el 2 + herramienta molu_help + system prompt y preámbulo de Horacio (spec 016), sin SKILL.",
    "",
    "RONDAS",
    "R1 (25-nov): flujo anterior de work_orders.* (30 funciones: avance con respuestas y firma, ConsumeFromOrder/ReturnLeftovers). Sus números NO son comparables con R2/R3.",
    "R2 (26-nov): flujo de ejecución de la rama work_orders de Seam (33 funciones). Claude (3 perfiles × 6 prompts × 2 repeticiones) y Gemini (1 repetición, hecha por el equipo).",
    "R3 (26-nov): perfil 4 con molu_help. Claude Opus 5.5 (2 repeticiones).",
    "R4 (27-nov): los mismos 4 perfiles con modelos Claude más simples: Sonnet 5.5 y Haiku 5.5 (2 repeticiones cada uno). El modelo por defecto del CLI en R2 y R3 es Opus 5.5 (verificado en la transcripción de R3).",
    "",
    "RÚBRICA DE CALIDAD (automática, sobre el estado final de xolu)",
    "perfecta = objetivo cumplido, sin preguntas, ≤ 2 errores de herramienta · buena = cumplido, sin preguntas, más de 2 errores · aceptable = cumplido pero preguntó · regular = a medias · mala = no cumplido.",
    "V2 termina en pregunta por un dato obligatorio (recepción confirmada): la rúbrica lo puntúa regular aunque sea la conducta correcta.",
    "",
    "LÍMITES",
    "Un modelo por fila; pocas repeticiones; verificador propio; el costo en USD para Gemini depende de lo que informe el runner. Los tiempos dependen de la carga de la máquina y de cachés.",
]: wn.append([line])
for c in wn["A"]:
    c.alignment = Alignment(wrap_text=True, vertical="top")
    if c.value in ("PERFILES", "RONDAS", "RÚBRICA DE CALIDAD (automática, sobre el estado final de xolu)", "LÍMITES"): c.font = Font(bold=True)
out = os.path.join(HERE, "planilla-benchmark.xlsx"); wb.save(out); print("guardado", out, "|", len(rows), "corridas,", len(groups), "grupos")
