"""Gemini REST API + MCP stdio runner for Spec 014 agent-profile benchmarks."""
import copy
import json
import os
import subprocess
import time
import urllib.parse
import requests

# Approximate list pricing in USD per 1M tokens (input, cached_input, output_incl_thinking).
# Used only when the Gemini API does not report USD directly; unrecognised models yield 0.0.
MODEL_PRICING_PER_M = {
    "gemini-2.5-flash": (0.30, 0.03, 2.50),
    "gemini-2.5-flash-preview": (0.30, 0.03, 2.50),
    "gemini-2.5-pro": (1.25, 0.125, 10.00),
    "gemini-2.5-pro-preview": (1.25, 0.125, 10.00),
    "gemini-3-flash-preview": (0.50, 0.05, 3.00),
    "gemini-3-pro-preview": (2.00, 0.20, 12.00),
    "gemini-3.1-pro-preview": (2.00, 0.20, 12.00),
    "gemini-2.0-flash": (0.10, 0.025, 0.40),
}


def estimate_cost_usd(model: str, prompt_tokens: int, cached_tokens: int, out_tokens: int, thought_tokens: int) -> float:
    base = model.split("/")[-1]
    rates = MODEL_PRICING_PER_M.get(base)
    if not rates:
        for prefix, r in MODEL_PRICING_PER_M.items():
            if base.startswith(prefix):
                rates = r
                break
    if not rates:
        return 0.0
    in_rate, cache_rate, out_rate = rates
    uncached = max(0, prompt_tokens - cached_tokens)
    usd = (uncached * in_rate + cached_tokens * cache_rate + (out_tokens + thought_tokens) * out_rate) / 1_000_000
    return round(usd, 4)


def sanitize_schema(schema: dict) -> dict:
    """Return a clean JSON Schema copy suitable for Gemini parametersJsonSchema."""
    if not isinstance(schema, dict):
        return {"type": "object", "properties": {}}
    s = copy.deepcopy(schema)
    s.pop("$schema", None)
    s.pop("examples", None)
    if s.get("type") == "object" and "properties" not in s:
        s["properties"] = {}
    return s


class MCPStdioClient:
    """Minimal synchronous JSON-RPC 2.0 MCP client over stdio for bin/molu."""

    def __init__(self, cmd: list[str], cwd: str, env: dict[str, str]):
        self.proc = subprocess.Popen(
            cmd,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            cwd=cwd,
            env=env,
        )
        self._next_id = 1
        self.initialize()

    def _send(self, payload: dict) -> None:
        assert self.proc.stdin is not None
        self.proc.stdin.write(json.dumps(payload) + "\n")
        self.proc.stdin.flush()

    def _recv(self, expected_id: int) -> dict:
        assert self.proc.stdout is not None
        while True:
            line = self.proc.stdout.readline()
            if not line:
                err = self.proc.stderr.read() if self.proc.stderr else ""
                raise RuntimeError(f"molu EOF waiting for id={expected_id}; stderr={err[-600:]}")
            line = line.strip()
            if not line:
                continue
            msg = json.loads(line)
            if msg.get("id") == expected_id:
                return msg

    def request(self, method: str, params: dict | None = None) -> dict:
        rid = self._next_id
        self._next_id += 1
        self._send({"jsonrpc": "2.0", "id": rid, "method": method, "params": params or {}})
        return self._recv(rid)

    def initialize(self) -> None:
        resp = self.request(
            "initialize",
            {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "clientInfo": {"name": "molu-bench-gemini", "version": "1.0"},
            },
        )
        if "error" in resp:
            raise RuntimeError(f"MCP initialize failed: {resp['error']}")
        self._send({"jsonrpc": "2.0", "method": "notifications/initialized", "params": {}})

    def list_tools(self, expect_domain: bool = False) -> list[dict]:
        deadline = time.time() + 5.0
        while True:
            resp = self.request("tools/list", {})
            tools = (resp.get("result") or {}).get("tools") or []
            if not expect_domain or len(tools) > 13 or time.time() >= deadline:
                return tools
            time.sleep(0.2)

    def call_tool(self, name: str, arguments: dict) -> tuple[str, bool]:
        resp = self.request("tools/call", {"name": name, "arguments": arguments or {}})
        if "error" in resp:
            return json.dumps(resp["error"], ensure_ascii=False), True
        res = resp.get("result") or {}
        parts = []
        for item in res.get("content") or []:
            if isinstance(item, dict) and "text" in item:
                parts.append(item["text"])
            else:
                parts.append(json.dumps(item, ensure_ascii=False))
        return "\n".join(parts), bool(res.get("isError"))

    def close(self) -> None:
        try:
            if self.proc.stdin:
                self.proc.stdin.close()
        except Exception:
            pass
        self.proc.terminate()
        try:
            self.proc.wait(timeout=3)
        except Exception:
            self.proc.kill()


def _load_skill(skill_path: str) -> tuple[str, str]:
    raw = open(skill_path, encoding="utf-8").read()
    desc = "Cómo operar Work Orders de Seam con las funciones work_orders.* del MCP molu."
    if raw.startswith("---"):
        parts = raw.split("---", 2)
        if len(parts) == 3:
            for line in parts[1].splitlines():
                if line.startswith("description:"):
                    desc = line.split(":", 1)[1].strip()
            return desc, parts[2].strip()
    return desc, raw


def _load_api_key(repo: str) -> str:
    k = os.environ.get("GEMINI_API_KEY") or os.environ.get("GOOGLE_API_KEY") or ""
    if k:
        return k
    env_path = os.path.join(repo, ".env")
    if os.path.exists(env_path):
        for raw_line in open(env_path, encoding="utf-8"):
            line = raw_line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            name, val = line.split("=", 1)
            name = name.strip()
            if name in ("GEMINI_API_KEY", "GOOGLE_API_KEY"):
                return val.strip().strip("\x27\"")
    return ""


def _post_generate_content(url: str, api_key: str, body: dict, timeout_s: float) -> tuple[dict, float]:
    headers = {"Content-Type": "application/json", "x-goog-api-key": api_key}
    backoff = 1.0
    for attempt in range(6):
        t0 = time.time()
        try:
            resp = requests.post(url, headers=headers, json=body, timeout=timeout_s)
        except requests.exceptions.RequestException as exc:
            if attempt < 5:
                time.sleep(backoff)
                backoff = min(backoff * 2, 16.0)
                continue
            raise RuntimeError(f"Gemini request failed: {exc}") from exc
        dt = time.time() - t0
        if resp.status_code in (429, 500, 502, 503, 504) and attempt < 5:
            time.sleep(backoff)
            backoff = min(backoff * 2, 16.0)
            continue
        if resp.status_code != 200:
            raise RuntimeError(f"Gemini HTTP {resp.status_code}: {resp.text[:800]}")
        return resp.json(), dt
    raise RuntimeError("Gemini API retries exhausted")


def run_gemini(
    profile: int,
    prompt: str,
    tag: str,
    *,
    repo: str,
    cwd: str,
    mcp_env: dict[str, str],
    priming: str,
    skill_path: str,
    raw_path: str,
    model: str = "gemini-3-flash-preview",
    max_turns: int = 60,
    timeout_s: int = 900,
    api_base: str | None = None,
) -> dict:
    api_key = _load_api_key(repo)
    if not api_key and not api_base:
        raise RuntimeError("Set GEMINI_API_KEY (or GOOGLE_API_KEY) to run the Gemini benchmark")


    base = (api_base or os.environ.get("GEMINI_API_BASE") or "https://generativelanguage.googleapis.com/v1beta").rstrip("/")
    url = f"{base}/models/{urllib.parse.quote(model, safe='')}:generateContent"

    t0 = time.time()
    mcp = MCPStdioClient([os.path.join(repo, "bin", "molu")], cwd=cwd, env=mcp_env)
    raw_events = []
    calls = errs = turns = 0
    api_s = 0.0
    used_skill = False
    timed_out = False
    tools_seen: list[str] = []
    in_tok = cached_tok = out_tok = thought_tok = 0
    final_text = ""

    try:
        mcp_tools = mcp.list_tools(expect_domain=(profile != 1))
        declarations = [
            {
                "name": t["name"],
                "description": t.get("description") or t["name"],
                "parametersJsonSchema": sanitize_schema(t.get("inputSchema") or {}),
            }
            for t in mcp_tools
        ]

        skill_desc, skill_body = ("", "")
        system_instruction = None
        if profile == 3:
            skill_desc, skill_body = _load_skill(skill_path)
            declarations.append(
                {
                    "name": "Skill",
                    "description": f"Carga las instrucciones completas de una skill disponible. Skills disponibles: work-orders-seam ({skill_desc})",
                    "parametersJsonSchema": {
                        "type": "object",
                        "properties": {
                            "skill": {
                                "type": "string",
                                "enum": ["work-orders-seam"],
                                "description": "Nombre de la skill a cargar.",
                            }
                        },
                        "required": ["skill"],
                        "additionalProperties": False,
                    },
                }
            )
            system_instruction = {
                "parts": [
                    {
                        "text": (
                            f"{priming}\n\n"
                            f"Skill disponible: `work-orders-seam` — {skill_desc} "
                            "Si la tarea trata de órdenes de trabajo, plantillas, bloques, cuadrillas, tercerizados, "
                            "depósitos, consumibles o stock, invoca `Skill` con {\"skill\": \"work-orders-seam\"} al inicio."
                        )
                    }
                ]
            }

        raw_events.append(
            {
                "type": "init",
                "runner": "gemini",
                "model": model,
                "profile": profile,
                "tools": [d["name"] for d in declarations],
            }
        )

        contents: list[dict] = [{"role": "user", "parts": [{"text": prompt}]}]

        while turns < max_turns:
            remaining = timeout_s - (time.time() - t0)
            if remaining <= 0:
                timed_out = True
                break

            body: dict = {
                "contents": contents,
                "tools": [{"functionDeclarations": declarations}],
            }
            if system_instruction is not None:
                body["systemInstruction"] = system_instruction

            turns += 1
            res, dt = _post_generate_content(url, api_key, body, timeout_s=min(120.0, remaining))
            api_s += dt

            usage = res.get("usageMetadata") or {}
            in_tok += int(usage.get("promptTokenCount") or 0)
            cached_tok += int(usage.get("cachedContentTokenCount") or 0)
            out_tok += int(usage.get("candidatesTokenCount") or 0)
            thought_tok += int(usage.get("thoughtsTokenCount") or 0)

            cand = (res.get("candidates") or [{}])[0]
            model_content = cand.get("content") or {"role": "model", "parts": []}
            parts = model_content.get("parts") or []

            raw_events.append(
                {
                    "type": "turn",
                    "turn": turns,
                    "api_s": round(dt, 3),
                    "finish_reason": cand.get("finishReason"),
                    "usage": usage,
                    "content": model_content,
                }
            )

            function_calls = [p["functionCall"] for p in parts if isinstance(p, dict) and "functionCall" in p]
            if not function_calls:
                final_text = "".join(
                    p.get("text", "")
                    for p in parts
                    if isinstance(p, dict) and "text" in p and not p.get("thought")
                ).strip()
                break

            # Preserve exact model content (including thought / thoughtSignature) for multi-turn tool calling.
            contents.append(model_content)

            resp_parts = []
            for fc in function_calls:
                fname = fc["name"]
                fargs = fc.get("args") or {}
                calls += 1
                tools_seen.append(fname)

                if fname == "Skill":
                    used_skill = True
                    sname = fargs.get("skill")
                    if sname == "work-orders-seam":
                        out_text, is_err = skill_body, False
                    else:
                        out_text, is_err = f"Unknown skill: {sname}", True
                else:
                    out_text, is_err = mcp.call_tool(fname, fargs)

                if is_err and "EMPTY" not in out_text:
                    errs += 1

                raw_events.append(
                    {
                        "type": "tool_result",
                        "turn": turns,
                        "name": fname,
                        "args": fargs,
                        "is_error": is_err,
                        "output": out_text,
                    }
                )

                fresp = {"name": fname, "response": {"result": out_text, "isError": is_err}}
                if fc.get("id"):
                    fresp["id"] = fc["id"]
                resp_parts.append({"functionResponse": fresp})
            contents.append({"role": "user", "parts": resp_parts})
        else:
            # Exhausted max_turns without a final text response
            final_text = final_text or ""

    finally:
        mcp.close()
        wall = time.time() - t0
        usd = estimate_cost_usd(model, in_tok, cached_tok, out_tok, thought_tok)
        raw_events.append(
            {
                "type": "result",
                "wall_s": round(wall, 1),
                "api_s": round(api_s, 1),
                "turns": turns,
                "tool_calls": calls,
                "tool_errors": errs,
                "input_tokens": in_tok,
                "cached_tokens": cached_tok,
                "output_tokens": out_tok,
                "thinking_tokens": thought_tok,
                "usd": usd,
                "used_skill": used_skill,
                "timeout": timed_out,
                "result": final_text,
            }
        )
        with open(raw_path, "w", encoding="utf-8") as f:
            for ev in raw_events:
                f.write(json.dumps(ev, ensure_ascii=False) + "\n")

    return dict(
        wall_s=round(wall, 1),
        api_s=round(api_s, 1),
        turns=turns,
        tool_calls=calls,
        tool_errors=errs,
        input_tokens=in_tok,
        cached_tokens=cached_tok,
        output_tokens=out_tok,
        thinking_tokens=thought_tok,
        usd=usd,
        used_skill=used_skill,
        timeout=timed_out,
        text=final_text,
        tools_seen=tools_seen,
        runner="gemini",
        model=model,
    )
