#!/usr/bin/env python3
"""Benchmark runner for spec 014: same prompts, three agent profiles, timings + state verification.

usage: run.py [--reps N] [--profiles 1,2,3] [--prompts L1,L2,G1,G2,V1,V2] [--tag NAME]
Each run is a fresh headless `claude -p` in an empty directory outside the repo with no built-in tools.
"""
import argparse, json, os, shutil, subprocess, sys, time

REPO = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
HERE = os.path.dirname(os.path.abspath(__file__))
WORK = os.environ.get("BENCH_WORK", "/tmp/molu-bench")
RESULTS = os.path.join(HERE, "results")

PROMPTS = {
    "L1": ("lectura", False, "¿Qué plantillas de órdenes de trabajo existen y qué estados tiene la de traslado de activos?"),
    "L2": ("lectura", False, "¿Hay algún consumible con stock bajo? ¿En qué depósito?"),
    "G1": ("guiada", True, "Crea una orden con la plantilla Demo WO - Traslado de activos para los equipos de prueba WO 1 y WO 2, asignada a la Cuadrilla Traslados, y llévala hasta el estado final: punto de llegada -34.90,-56.16, fecha 2026-11-24, recepción confirmada, firma Fabricio."),
    "G2": ("guiada", True, "Crea una orden con la plantilla de mantenimiento preventivo para el equipo de prueba WO 1 asignada a la Cuadrilla Mantenimiento, carga 10 unidades de Filtro de prueba WO en el Depósito Central, transfiere 4 al almacén móvil de la orden, consume 1 (usuario 1) y devuelve el resto al Depósito Central."),
    "V1": ("vaga", True, "Hay stock bajo de algo. Reponelo para que quede por encima del mínimo."),
    "V2": ("vaga", True, "Hay una orden en el tablero sin dueño. Dejala en manos de quien corresponda y avanzala un paso."),
}

PRIMING = ("Eres un asistente de operaciones de mantenimiento que trabaja sobre Seam (gestión de activos y órdenes de trabajo) "
           "a través del MCP molu. Usa primero las funciones de dominio work_orders.* y recurre a las primitivas genéricas solo si no "
           "hay una función adecuada. Si un dato se puede deducir consultando (List*/Get*), consúltalo en vez de preguntar al usuario. "
           "Actúa hasta terminar la tarea y resume al final qué quedó hecho (ids, estados, stock). Responde en español.")

def mcp_config(profile):
    c = json.load(open(os.path.expanduser("~/.claude.json")))["projects"][REPO]["mcpServers"]["molu"]
    env = dict(c["env"])
    if profile == 1:  # no Hub: only the 13 generic xolu primitives
        for k in list(env):
            if "HUB" in k or k.startswith("MOLU_FRONT_TOKEN_") or k == "MOLU_FRONT_DEFAULT_TENANT":
                env.pop(k)
    path = os.path.join(WORK, f"mcp-p{1 if profile == 1 else 2}.json")
    json.dump({"mcpServers": {"molu": {"command": c["command"], "args": [], "env": env}}}, open(path, "w"))
    return path

def workdir(profile):
    d = os.path.join(WORK, f"cwd-p{profile}")
    shutil.rmtree(d, ignore_errors=True); os.makedirs(d)
    if profile == 3:
        dst = os.path.join(d, ".claude", "skills", "work-orders-seam"); os.makedirs(dst)
        shutil.copy(os.path.join(HERE, "skill", "work-orders-seam", "SKILL.md"), dst)
    return d

def reset():
    subprocess.run([os.path.join(REPO, ".dev-xolu", "demo-reset.sh")], cwd=REPO, capture_output=True, timeout=600)

def wo(tool, args):
    p = subprocess.run(["python3", os.path.join(REPO, ".dev-xolu", "wo.py"), "work_orders." + tool, json.dumps(args), "10000000"],
                       cwd=REPO, capture_output=True, text=True, timeout=120)
    try:
        r = json.loads(p.stdout); t = r["content"][0]["text"]
        return json.loads(t) if t.startswith("{") else {"_raw": t}, bool(r.get("isError"))
    except Exception as e:
        return {"_raw": p.stdout[:200] + str(e)}, True

def orders():
    r, _ = wo("ListOrders", {}); return r.get("orders", [])

def verify(pid, text):
    """Return (score in {0,0.5,1}, detail) from the final xolu state / answer text."""
    if pid == "L1":
        n = sum(s in text for s in ("Preparada", "En traslado", "Entregada")); return (1 if n == 3 else 0.5 if n else 0), f"estados {n}/3"
    if pid == "L2":
        a, c = "Aceite" in text, "Central" in text; return (1 if a and c else 0.5 if a else 0), f"aceite={a} central={c}"
    if pid == "G1":
        cand = [o for o in orders() if str(o.get("template_id")) == "3" and "tablero" not in o.get("title", "") and str(o.get("responsible_id")) == "2"]
        if not cand: return 0, "no hay orden de traslado asignada a la cuadrilla 2"
        o = cand[0]; full = set(map(str, o.get("asset_ids", []))) == {"19", "20"}
        if o.get("terminal"):
            d, _ = wo("GetOrder", {"order_id": o["id"]}); ans = json.dumps(d.get("block_answers", {}))
            return (1 if full and "-34.9" in ans else 0.5), f"terminal, activos_ok={full}, respuestas={ans[:80]}"
        return 0.5, f"orden creada pero estado={o.get('state')}"
    if pid == "G2":
        cand = [o for o in orders() if str(o.get("template_id")) == "1"]
        if not cand: return 0, "no hay orden de mantenimiento"
        d, _ = wo("GetConsumable", {"consumable_id": "1"}); st = d.get("stock", [])
        central = sum(s["quantity"] for s in st if str(s.get("warehouse_id")) == "1"); mobile = sum(s["quantity"] for s in st if s.get("mobile"))
        ok = central == 9 and mobile == 0 and str(cand[0].get("responsible_id")) == "1"
        return (1 if ok else 0.5), f"central={central} movil={mobile} cuadrilla={cand[0].get('responsible_id')}"
    if pid == "V1":
        d, err = wo("ListLowStock", {}); empty = err and "EMPTY" in json.dumps(d)
        return (1 if empty else 0), ("sin alertas" if empty else "sigue bajo")
    if pid == "V2":
        cand = [o for o in orders() if "tablero" in o.get("title", "")]
        if not cand: return 0, "no encuentro la orden del tablero"
        o = cand[0]; assigned = bool(o.get("responsible_id")); adv = int(o.get("workflow_index", 0) or 0) >= 1
        return (1 if assigned and adv else 0.5 if assigned else 0), f"responsable={o.get('responsible_id')} indice={o.get('workflow_index')}"
    return 0, "?"

def quality(score, asked, errors):
    if score == 0: return "mala"
    if score < 1: return "regular"
    if asked: return "aceptable"
    return "perfecta" if errors <= 2 else "buena"

def run_claude(profile, prompt, tag):
    cwd = workdir(profile)
    cmd = ["claude", "-p", prompt, "--output-format", "stream-json", "--verbose", "--strict-mcp-config", "--mcp-config", mcp_config(profile),
           "--setting-sources", "project" if profile == 3 else "", "--no-session-persistence", "--max-turns", "60"]
    if profile == 3:
        cmd += ["--tools", "Skill", "--allowedTools", "mcp__molu", "Skill", "--append-system-prompt", PRIMING]
    else:
        cmd += ["--tools", "", "--allowedTools", "mcp__molu", "--disable-slash-commands"]
    t0 = time.time()
    try:
        p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=900, stdin=subprocess.DEVNULL)
        out, timeout = p.stdout, False
    except subprocess.TimeoutExpired as e:
        out, timeout = (e.stdout or b"").decode() if isinstance(e.stdout, bytes) else (e.stdout or ""), True
    wall = time.time() - t0
    open(os.path.join(RESULTS, "raw", tag + ".jsonl"), "w").write(out)
    calls = errs = 0; skill = False; res = {}; tools_seen = []
    for line in out.splitlines():
        try: ev = json.loads(line)
        except Exception: continue
        if ev.get("type") == "assistant":
            for b in ev["message"].get("content", []):
                if b.get("type") == "tool_use":
                    calls += 1; tools_seen.append(b["name"])
                    if b["name"] == "Skill": skill = True
        elif ev.get("type") == "user":
            for b in (ev["message"].get("content") or []):
                if isinstance(b, dict) and b.get("type") == "tool_result" and b.get("is_error") and "EMPTY" not in json.dumps(b.get("content")):
                    errs += 1
        elif ev.get("type") == "result": res = ev
    return dict(wall_s=round(wall, 1), api_s=round(res.get("duration_api_ms", 0) / 1000, 1), turns=res.get("num_turns"), tool_calls=calls,
                tool_errors=errs, usd=round(res.get("total_cost_usd", 0) or 0, 3), used_skill=skill, timeout=timeout,
                text=res.get("result") or "", tools_seen=tools_seen)

def main():
    ap = argparse.ArgumentParser(); ap.add_argument("--reps", type=int, default=1); ap.add_argument("--profiles", default="1,2,3")
    ap.add_argument("--prompts", default="L1,L2,G1,G2,V1,V2"); ap.add_argument("--tag", default="run")
    a = ap.parse_args(); os.makedirs(os.path.join(RESULTS, "raw"), exist_ok=True); os.makedirs(WORK, exist_ok=True)
    out = open(os.path.join(RESULTS, f"{a.tag}.jsonl"), "a")
    for rep in range(1, a.reps + 1):
        for prof in map(int, a.profiles.split(",")):
            fresh = False
            for pid in a.prompts.split(","):
                kind, mutates, prompt = PROMPTS[pid]
                if mutates or not fresh: reset(); fresh = True
                tag = f"{a.tag}-p{prof}-{pid}-r{rep}"
                r = run_claude(prof, prompt, tag)
                score, detail = verify(pid, r["text"])
                asked = "?" in r["text"][-400:]
                row = dict(profile=prof, prompt=pid, kind=kind, rep=rep, score=score, detail=detail, asked=asked,
                           quality=quality(score, asked, r["tool_errors"]), **{k: v for k, v in r.items() if k not in ("text", "tools_seen")},
                           answer=r["text"][:600])
                out.write(json.dumps(row, ensure_ascii=False) + "\n"); out.flush()
                print(f"p{prof} {pid} r{rep}: {row['quality']:9s} {row['wall_s']:6.1f}s  turns={row['turns']} calls={row['tool_calls']} err={row['tool_errors']} skill={row['used_skill']} | {detail}", flush=True)
    reset()

if __name__ == "__main__":
    main()
