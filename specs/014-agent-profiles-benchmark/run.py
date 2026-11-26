#!/usr/bin/env python3
"""Benchmark runner for spec 014: same prompts, three agent profiles, timings + state verification.
Updated 26-nov-2026 for the execution model of work_orders.* (block responsibles, header, blocks, consumption and settlement).

usage: run.py [--reps N] [--profiles 1,2,3,4] [--prompts L1,L2,G1,G2,V1,V2] [--tag NAME]
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
    "G1": ("guiada", True, "Crea una orden con la plantilla Demo WO - Traslado de activos para los equipos de prueba WO 1 y WO 2, asignada a la Cuadrilla Traslados, y llévala hasta el estado final: punto de llegada -34.90,-56.16, fecha 2026-11-24, recepción confirmada."),
    "G2": ("guiada", True, "Crea una orden con la plantilla de mantenimiento preventivo para el equipo de prueba WO 1, asignada a la Cuadrilla Mantenimiento (la preparación a cargo del usuario 1). Carga 10 unidades de Filtro de prueba WO en el Depósito Central, lleva 4 al almacén móvil de la orden y llévala hasta el estado final. Al terminar, registra que se usó 1 (usuario 1) y devuelve el resto al Depósito Central."),
    "V1": ("vaga", True, "Hay stock bajo de algo. Reponelo para que quede por encima del mínimo."),
    "V2": ("vaga", True, "Hay una orden en el tablero sin dueño. Dejala en manos de quien corresponda y avanzala un paso."),
}

PRIMING = ("Eres un asistente de operaciones de mantenimiento que trabaja sobre Seam (gestión de activos y órdenes de trabajo) "
           "a través del MCP molu. Usa primero las funciones de dominio work_orders.* y recurre a las primitivas genéricas solo si no "
           "hay una función adecuada. Si un dato se puede deducir consultando (List*/Get*), consúltalo en vez de preguntar al usuario. "
           "Actúa hasta terminar la tarea y resume al final qué quedó hecho (ids, estados, stock). Responde en español.")

sys.path.insert(0, HERE)

def mcp_env(profile):
    env = dict(os.environ)
    env["MOLU_FRONT_XOLU_URL"] = os.environ.get("BENCH_XOLU_URL", "http://127.0.0.1:9898")
    env["MOLU_FRONT_XOLU_AUTH_MODE"] = "bearer"
    env["MOLU_FRONT_XOLU_TOKEN_FILE"] = os.path.join(REPO, ".dev-xolu", ".token")
    env["MOLU_FRONT_TELEMETRY_ENABLED"] = "false"
    env["MOLU_FRONT_LOG_LEVEL"] = "error"
    env["MOLU_FRONT_CALL_TIMEOUT"] = "30s"
    env["MOLU_FRONT_DOMAIN_CALL_TIMEOUT"] = "30s"
    if profile == 1:
        for k in list(env):
            if "HUB" in k or k.startswith("MOLU_FRONT_TOKEN_") or k == "MOLU_FRONT_DEFAULT_TENANT":
                env.pop(k)
    else:
        env["MOLU_FRONT_HUB_URL"] = os.environ.get("BENCH_HUB_URL", "http://127.0.0.1:8391")
        env["MOLU_FRONT_HUB_AUTH_MODE"] = "none"
        env["MOLU_FRONT_DEFAULT_TENANT"] = os.environ.get("MOLU_FRONT_DEFAULT_TENANT", "dev")
        env["MOLU_FRONT_TOKEN_ORDERS_API_TOKEN"] = os.environ.get("BENCH_ORDERS_TOKEN", "bench-orders-token")
    return env

# Profile 4 (Horacio's idea, 25-nov-2026): a fixed system prompt, a short preamble in front of every user message
# and the molu_help tool, with no SKILL. The texts are his, verbatim.
SYSTEM4 = ("You are a helpful assistant in logistics and physical assets management, helping with the management and the operation of Seam AMS, "
           "our assets management system. To that end, you have several functions available to you through the molu MCP tool. The user assumes you will "
           "be using molu for most things, and you can use the molu_help function to learn anything about the molu tool, and Seam AMS operations.")
PREAMBLE4 = ("Use the molu MCP tool to answer the following user request. When in doubt use molu's molu_help function. "
             "The following is what the user said:\n")

def mcp_config(profile):
    c = json.load(open(os.path.expanduser("~/.claude.json")))["projects"][REPO]["mcpServers"]["molu"]
    env = dict(c["env"])
    # Optional isolated stack (does not touch the dev xolu): BENCH_XOLU_URL / BENCH_HUB_URL.
    if os.environ.get("BENCH_XOLU_URL"): env["MOLU_FRONT_XOLU_URL"] = os.environ["BENCH_XOLU_URL"]
    if os.environ.get("BENCH_HUB_URL"): env["MOLU_FRONT_HUB_URL"] = os.environ["BENCH_HUB_URL"]
    if profile == 1:  # no Hub: only the 13 generic xolu primitives
        for k in list(env):
            if "HUB" in k or k.startswith("MOLU_FRONT_TOKEN_") or k == "MOLU_FRONT_DEFAULT_TENANT":
                env.pop(k)
    # molu_help exists only in profile 4; the other profiles must not see it.
    env["MOLU_FRONT_HELP_ENABLED"] = "true" if profile == 4 else "false"
    if profile == 4:  # the domain texts ship with their publishers; the built-in ones are generic
        env["MOLU_FRONT_HELP_DIR"] = os.pathsep.join(os.path.join(REPO, "pkg", "publisher", p, "help") for p in ("orders", "issues"))
    command = os.environ.get("BENCH_MOLU_BIN") or c["command"]
    path = os.path.join(WORK, f"mcp-p{4 if profile == 4 else 1 if profile == 1 else 2}.json")
    json.dump({"mcpServers": {"molu": {"command": command, "args": [], "env": env}}}, open(path, "w"))
    return path

def workdir(profile):
    d = os.path.join(WORK, f"cwd-p{profile}")
    shutil.rmtree(d, ignore_errors=True); os.makedirs(d)
    if profile == 3:
        dst = os.path.join(d, ".claude", "skills", "work-orders-seam"); os.makedirs(dst)
        shutil.copy(os.path.join(HERE, "skill", "work-orders-seam", "SKILL.md"), dst)
    return d

def reset():
    # BENCH_RESET: command that leaves the (isolated) stack in the demo state; default is the dev stack's demo-reset.sh.
    cmd = os.environ.get("BENCH_RESET") or os.path.join(REPO, ".dev-xolu", "demo-reset.sh")
    p = subprocess.run([cmd], cwd=REPO, capture_output=True, text=True, timeout=600)
    if p.returncode != 0: print("RESET FAILED:", (p.stdout + p.stderr)[-400:], flush=True)

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
        d, _ = wo("GetOrder", {"order_id": o["id"]})
        blocks_ok = bool(d.get("blocks")) and all(b.get("state") == "completed" for b in d["blocks"])
        header_ok = bool(d.get("header_confirmed_at")) and (d.get("header_values") or {}).get("recepcion_confirmada") is True
        ans = json.dumps(d.get("block_answers", {})); answers_ok = "-34.9" in ans and "2026-11-24" in ans
        if o.get("terminal"):
            ok = full and blocks_ok and header_ok and answers_ok
            return (1 if ok else 0.5), f"terminal, activos_ok={full}, bloques_ok={blocks_ok}, cabecera_ok={header_ok}, respuestas={ans[:80]}"
        return 0.5, f"orden creada pero estado={o.get('state')}"
    if pid == "G2":
        cand = [o for o in orders() if str(o.get("template_id")) == "1"]
        if not cand: return 0, "no hay orden de mantenimiento"
        o = cand[0]; d, _ = wo("GetOrder", {"order_id": o["id"]})
        line = next((c for c in d.get("consumables", []) if str(c.get("consumable_id")) == "1"), {})
        settled = line.get("settled") is True and line.get("used") == 1 and line.get("returned") == 3
        g, _ = wo("GetConsumable", {"consumable_id": "1"}); st = g.get("stock", [])
        central = sum(s["quantity"] for s in st if str(s.get("warehouse_id")) == "1"); mobile = sum(s["quantity"] for s in st if s.get("mobile"))
        ok = central == 9 and mobile == 0 and settled and o.get("terminal") and str(o.get("responsible_id")) == "1"
        return (1 if ok else 0.5), f"central={central} movil={mobile} liquidado={settled} terminal={o.get('terminal')} cuadrilla={o.get('responsible_id')}"
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
    if profile == 4: prompt = PREAMBLE4 + prompt
    cmd = ["claude", "-p", prompt, "--output-format", "stream-json", "--verbose", "--strict-mcp-config", "--mcp-config", mcp_config(profile),
           "--setting-sources", "project" if profile == 3 else "", "--no-session-persistence", "--max-turns", "60"]
    if profile == 3:
        cmd += ["--tools", "Skill", "--allowedTools", "mcp__molu", "Skill", "--append-system-prompt", PRIMING]
    elif profile == 4:
        cmd += ["--tools", "", "--allowedTools", "mcp__molu", "--disable-slash-commands", "--append-system-prompt", SYSTEM4]
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
    calls = errs = helps = 0; skill = False; res = {}; tools_seen = []
    for line in out.splitlines():
        try: ev = json.loads(line)
        except Exception: continue
        if ev.get("type") == "assistant":
            for b in ev["message"].get("content", []):
                if b.get("type") == "tool_use":
                    calls += 1; tools_seen.append(b["name"])
                    if b["name"] == "Skill": skill = True
                    if b["name"].endswith("molu_help"): helps += 1
        elif ev.get("type") == "user":
            for b in (ev["message"].get("content") or []):
                if isinstance(b, dict) and b.get("type") == "tool_result" and b.get("is_error") and "EMPTY" not in json.dumps(b.get("content")):
                    errs += 1
        elif ev.get("type") == "result": res = ev
    return dict(wall_s=round(wall, 1), api_s=round(res.get("duration_api_ms", 0) / 1000, 1), turns=res.get("num_turns"), tool_calls=calls,
                tool_errors=errs, usd=round(res.get("total_cost_usd", 0) or 0, 3), used_skill=skill, help_calls=helps, timeout=timeout,
                text=res.get("result") or "", tools_seen=tools_seen)

def resolve_runner(arg):
    if arg != "auto":
        return arg
    if shutil.which("claude") and os.path.exists(os.path.expanduser("~/.claude.json")):
        return "claude"
    return "gemini"

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--reps", type=int, default=1)
    ap.add_argument("--profiles", default="1,2,3")
    ap.add_argument("--prompts", default="L1,L2,G1,G2,V1,V2")
    ap.add_argument("--tag", default="run")
    ap.add_argument("--runner", choices=["auto", "gemini", "claude"], default="auto")
    ap.add_argument("--model", default=os.environ.get("GEMINI_MODEL", "gemini-3-flash-preview"))
    ap.add_argument("--max-turns", type=int, default=60)
    ap.add_argument("--timeout", type=int, default=900)
    a = ap.parse_args()
    runner = resolve_runner(a.runner)
    os.makedirs(os.path.join(RESULTS, "raw"), exist_ok=True)
    os.makedirs(WORK, exist_ok=True)
    out = open(os.path.join(RESULTS, f"{a.tag}.jsonl"), "a")
    for rep in range(1, a.reps + 1):
        for prof in map(int, a.profiles.split(",")):
            fresh = False
            for pid in a.prompts.split(","):
                kind, mutates, prompt = PROMPTS[pid]
                if mutates or not fresh: reset(); fresh = True
                tag = f"{a.tag}-p{prof}-{pid}-r{rep}"
                if runner == "gemini":
                    from gemini_runner import run_gemini  # lazy: needs `requests`, which the Claude runner does not
                    r = run_gemini(
                        prof, prompt, tag,
                        repo=REPO, cwd=workdir(prof), mcp_env=mcp_env(prof),
                        priming=PRIMING, skill_path=os.path.join(HERE, "skill", "work-orders-seam", "SKILL.md"),
                        raw_path=os.path.join(RESULTS, "raw", tag + ".jsonl"),
                        model=a.model, max_turns=a.max_turns, timeout_s=a.timeout,
                    )
                else:
                    r = run_claude(prof, prompt, tag)
                score, detail = verify(pid, r["text"])
                asked = "?" in r["text"][-400:]
                row = dict(profile=prof, prompt=pid, kind=kind, rep=rep, score=score, detail=detail, asked=asked,
                           quality=quality(score, asked, r["tool_errors"]), **{k: v for k, v in r.items() if k not in ("text", "tools_seen")},
                           answer=r["text"][:600])
                out.write(json.dumps(row, ensure_ascii=False) + "\n"); out.flush()
                print(f"[{runner}] p{prof} {pid} r{rep}: {row['quality']:9s} {row['wall_s']:6.1f}s  turns={row['turns']} calls={row['tool_calls']} err={row['tool_errors']} skill={row['used_skill']} help={row.get('help_calls', 0)} | {detail}", flush=True)
    reset()

if __name__ == "__main__":
    main()
