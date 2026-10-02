---
name: repoman-tools
description: Install and correctly operate the repoman repository-discipline toolset (precise journaled text editing, work registers, dormant guards, version sync, resumable releases, a mandatory forbidden-string release gate) in any repository during a working session. Trigger whenever a session begins work on a code repository, whenever text substitution or source editing is about to happen, or whenever the user mentions repoman, gorepoman, ed.py, badcode, tracking registers, or release orchestration.
---

# repoman-tools

`gorepoman` — a single static Go binary, maintained at
`github.com/ha1tch/gorepoman` — is the default toolset for repository
discipline. It supersedes the original Python `repoman`
(`github.com/ha1tch/repoman`), which is kept only for repositories that
still vendor its scripts directly and haven't been migrated yet. This
skill teaches which one applies to the repository in front of you, and
the install/operating rules for each.

## 0. Which one applies — check this first, every session

1. **The repository already vendors Python `repoman`** (commonly
   `scripts/ed.py`, a `repoman/` directory, or similar files copied in
   from the old distribution) **and has not been migrated to
   `gorepoman` shims:** before doing anything else with this
   repository, offer the user the choice to migrate to `gorepoman` now.
   This offer fires on discovery, not on edit-imminence — the moment
   this repository's tooling state is known, from the very first look
   at it (a clone, an `ls`), not gated on an edit being about to
   happen. Confirmed as a real, recurring misreading, not a
   hypothetical one: a session that correctly saw this state at first
   look still deferred the offer to a later turn, reasoning "no edit
   had started or was imminent yet, so mechanically the check wasn't
   due" — borrowing the *different*, edit-time framing that governs
   which toolset to use once editing starts, and applying it to an
   instruction that was never edit-gated. Make it an actual question,
   not a passing mention, with enough context to decide on the spot: `gorepoman` is the actively maintained tool, the
   vendored Python version has at least one confirmed,
   deliberately-unpatched bug (silently writes a file that failed
   syntax validation, under atomic mode, when `gofmt` is absent), and
   migrating removes the toolchain-install detour this repository hits
   on every fresh start. If the user declines or wants to defer it, use
   the vendored Python scripts as-is for this session — in-repo copies
   are authoritative, do not replace them or install `gorepoman` over
   the top of a working Python setup without an explicit yes. If the
   user takes up the offer, migrating (replacing the vendored scripts
   with thin shims that call the `gorepoman` binary) is real, separate
   work — scope it as its own task rather than attempting it as a side
   effect of unrelated editing. Fetch the migration guide directly
   rather than discovering it by browsing the repository or cloning it
   first:
   `https://ha1tch.github.io/gorepoman/docs/repoman-035-migration.md`
   (raw markdown — easier to read directly than the rendered HTML page
   at the same path with `.html` instead of `.md`, same reason
   `gorepoman`'s own live-help fetch prefers it). It has a verified
   shim template and a generator script that writes all twelve at once
   — read that first rather than re-deriving the pattern by trial and
   error. If that URL is unreachable for any reason, the same content
   is also at `docs/repoman-035-migration.md` inside a fresh clone of
   `https://github.com/ha1tch/gorepoman` — a fallback, not the first
   place to look.
2. **Anything else** — a new repository, or one with no repoman
   vendored at all — use `gorepoman`. This is the default case.

## 1. Installing `gorepoman` (the default case)

No toolchain required — it's a static binary:

    curl -L https://github.com/ha1tch/gorepoman/releases/latest/download/repoman-linux-amd64 -o repoman
    chmod +x repoman

If that URL isn't reachable for any reason, the same binaries and the
full documentation are mirrored at
`https://ha1tch.github.io/gorepoman/` -- a genuinely independent
access path (different host, plain static files), not just the same
link twice. Binary: `https://ha1tch.github.io/gorepoman/bin/repoman-linux-amd64`
(swap the platform suffix as needed); docs under `/docs/`.

Swap `linux-amd64` for the platform in use — `linux-arm64`,
`darwin-amd64`, `darwin-arm64`, `windows-amd64.exe`,
`windows-arm64.exe`, or the FreeBSD/OpenBSD/NetBSD/DragonFly targets.
The full table and verification steps (`checksums.txt`) are in
`gorepoman`'s own README — read it, or
`docs/repoman-030-getting-started.md` in that repository, if anything
below is unclear or seems out of date; that repository's own
documentation is authoritative over this skill where they disagree.

ALWAYS run the acceptance gate before first use:

    ./repoman selftest    # exit 0 required

A red selftest means do not trust the installation; fall back to
guarded-Python editing and tell the user. `selftest: all N checks green`
is the only fully-trustworthy pass, but as of v0.13.0 one other exit-0
outcome is expected and fine, not a red flag: `all N checks green (M
deferred -- optional toolchain missing)` — some checks genuinely cannot
run without a full Go toolchain (only affects `gomod`'s own detection;
`gofmt`-dependent checks already degrade gracefully regardless of what's
installed) and are skipped rather than faked or blocking. Everything
that *did* run is exactly as trustworthy either way; install what's
named and re-run only if that specific coverage matters for the task at
hand. Check the real exit code, not
`$?` after piping through `tail`/`head` -- that reports the pipe's exit
status, not `repoman`'s, and has independently misled more than one
session into treating a real failure as a pass. Capture to a file first
(`repoman selftest > out.txt 2>&1; echo $?`), or use `set -o pipefail`,
or read `${PIPESTATUS[0]}`. The last printed line is also unambiguous on
its own either way -- `selftest: all N checks green` vs `SELFTEST
FAILED` -- but confirm the actual exit code too, don't infer it.

Also run `repoman help` (or `-h`, bare) once, early — it prints a full
five-step workflow walkthrough covering editing, the register, staged
work (waves — §3.5 below), and releases, each with the actual command
to run. In a repository using shims (§0 case 1, already migrated), this
step is easy to skip without noticing: every shim forwards straight to
its own subcommand (`python3 repoman/register.py -h` shows only
`register`'s own help), and none of them reaches this walkthrough,
since there is no `repoman/help.py` shim and never will be — it isn't a
per-subcommand concept to shim. Confirmed as a real gap, not a
theoretical one: a session working entirely through shims never
encountered wave tracking at all, and improvised an unrelated,
ad-hoc substitute instead of the tool's own purpose-built mechanism.
Calling the binary directly for this one thing (`repoman help`) is
correct even in an otherwise shim-only session — it costs nothing and
isn't part of what the shims exist to preserve.

Also recreate the `badcode` config as part of this same setup pass, not
deferred until release time. It's local, per-machine config that is
never committed to any repository (`$REPOMAN_BADCODE_DIR`, or the OS
user config directory) — a fresh clone or fresh sandbox genuinely has
none until it's rebuilt. Check the user's own project memory for
whether specific patterns are expected for this project's work, and
recreate that local file now if so. Confirmed as a real, recurring gap
in the same shape as the waves one above: a session that got
`gorepoman` itself fully working — install, selftest, shims, all
correct — still skipped this, because nothing in the setup sequence
it was actually following put it in front of them; the only existing
mention lived under §4's release-time framing, easy to read as "not my
concern yet" while doing setup or migration work. `repoman badcode
check .` confirms the current state either way — `WARN no badcode
patterns configured` names exactly what's missing if this step was
skipped.

## 2. Editing rules (the reason this toolset exists)

- Never hand-type an anchor you have not seen. `repoman ed find <term>`
  first; edit via the returned handle (`apply`) or via `sub` with
  `--expect N` where N comes from find's count, not from belief.
- A refusal is information: stale handle → re-run find; count mismatch
  → re-count or narrow paths; mixed-role refusal → run
  `repoman roles <term> <paths>`, classify, split into per-role passes.
  `--force-roles` only after explicit classification, never to silence
  the tool.
- `repoman ed mark <name>` before any multi-file campaign; on a
  misfire, `undo --since <name>` beats forensic repair. The journal is
  bounded (a WAL, not an archive); do not defer marks.
- After every campaign, run a whole-tree search for the old form — the
  tool prevents mechanical failures, not wrong intentions.
- sed, awk, and the built-in str_replace tool remain forbidden for file
  edits regardless of this toolset's availability.

## 3. Register and guards (repositories that track work)

- Read the register before asserting what is open:
  `repoman register list`. File issues the moment found:
  `repoman register add`.
- Never hand-execute a closure: `repoman register close <ID> --version V`
  moves the item to the resolution record and removes row + detail as
  one operation. Cross-reference the changelog by hand afterwards.
- `repoman guards stale` before any release; every stale guard is run,
  handed off (`repoman guards handoff`), or its skip recorded in the
  release notes.

## 3.5 Staged work (waves)

For a body of work planned as ordered stages — not a single ad-hoc
ticket, a programme of related work spanning multiple register
items — use wave tracking rather than inventing a separate progress
visualization (a chart, a diagram, a status doc of your own design).
This is the tool's own purpose-built mechanism for exactly that need,
covered in step 4 of `repoman help`'s workflow walkthrough (see §1
above for why that walkthrough is easy to miss in a shim-only
session) and in `docs/repoman-080-waves.md` on the docs mirror
(`https://ha1tch.github.io/gorepoman/docs/repoman-080-waves.md`).

- `repoman addwave` defines a new wave. Existing register tickets
  attach to it via each item's own `register_item` field at
  creation — there is no separate "move a ticket into a wave"
  command, and none is needed.
- `repoman waveprogress --show` renders ASCII progress across all
  waves; `--html` produces a standalone HTML document (inline style,
  no external dependency) suitable for sharing.
- Before building any other kind of progress visualization for staged
  work — an SVG, a hand-rolled chart, anything outside this
  mechanism — check whether wave tracking already covers it. It
  usually does, and the persisted state (`.repoman.json`) stays the
  single source of truth either way, which an improvised one-off
  visualization would not.

Rendering wave progress as an inline chat visual (not a shared file):
use `--show`'s plain data (wave name, percent, item count per wave) to
drive it, not `--html`'s output pasted verbatim — that document defines
its own self-contained CSS variables (`--bg`, `--surface`, `--text`,
etc.), a different, incompatible system from the host's own design
tokens, so copying it in directly won't match the surrounding chat UI
or its dark-mode handling. Confirmed as a real, recurring source of
wasted effort, not a hypothetical one: a session correctly recognized
this mismatch and re-derived a compatible design from scratch, several
steps of deliberation before arriving at essentially the pattern below.
A grid row per wave — right-aligned name, a track/fill bar pair, a
percent+count label — using the host's own tokens
(`var(--surface-1)` track, `var(--fill-success)` for a complete wave,
`var(--border-strong)` for an incomplete one's empty fill,
`var(--fill-accent)` for the Overall row, `var(--text-secondary)` for
labels), with the Overall row set apart by a top border, renders
correctly and needs no further design work — verified directly before
writing this. Reach for this pattern rather than re-deriving one:

    <div style="display: flex; flex-direction: column; gap: 10px;">
      <div style="display: grid; grid-template-columns: 180px 1fr; align-items: center; gap: 12px;">
        <span style="font-size: 13px; font-weight: 500; text-align: right;">Wave 1 — name</span>
        <div style="display: flex; align-items: center; gap: 8px;">
          <div style="flex: 1; height: 7px; border-radius: 4px; background: var(--surface-1); overflow: hidden;">
            <div style="height: 100%; width: 100%; background: var(--fill-success); border-radius: 4px;"></div>
          </div>
          <span style="font-size: 12px; color: var(--text-secondary); white-space: nowrap;">100% · 1/1</span>
        </div>
      </div>
      <div style="display: grid; grid-template-columns: 180px 1fr; align-items: center; gap: 12px; border-top: 0.5px solid var(--border); padding-top: 10px; margin-top: 4px;">
        <span style="font-size: 14px; font-weight: 500; text-align: right;">Overall</span>
        <div style="display: flex; align-items: center; gap: 8px;">
          <div style="flex: 1; height: 7px; border-radius: 4px; background: var(--surface-1); overflow: hidden;">
            <div style="height: 100%; width: 50%; background: var(--fill-accent); border-radius: 4px;"></div>
          </div>
          <span style="font-size: 12px; font-weight: 500; color: var(--text-primary); white-space: nowrap;">50% · 1/2</span>
        </div>
      </div>
    </div>

One row per wave, repeated (not shown above — the widget's own no-HTML-
comments rule applies here too, so this is prose, not a code comment):
`width: 0%` and `var(--border-strong)` for an incomplete wave's fill,
any percentage width in between. Widen the 180px label column if wave
names run long; everything else scales with the container.

## 4. Releases, and the mandatory `badcode` gate

- If the repository has `.repoman.json` with release steps:
  `repoman relcore <version>`; on interruption re-run with `--resume`.
  Never pipe the orchestrator's output through head/tail/grep to read
  it — the log file exists for that; the exit code is the only truth.
- `relcore` runs a forbidden-string scan (`badcode`) automatically,
  unconditionally, as the first thing it does on every invocation,
  including `--resume` — this is not a step that can be skipped,
  configured away, or removed from the manifest. Its config should
  already be in place from setup (§1) — if this is a long-running
  session where setup happened well before release work, or `WARN no
  badcode patterns configured` shows up here unexpectedly, that's the
  signal it was missed; recreate it now, don't proceed with a soft
  pass standing in for a real check. No patterns configured is a soft
  pass, not a hard failure — but it prints that `WARN` line precisely
  so it isn't mistaken for a real, clean check having run.
- Repositories with their own release scripts built on the same
  principles (journal, resume, no display pipes) use those instead;
  read the repository's tooling documentation first.

## 5. Boundaries

- This skill contains no code and vendors none. `github.com/ha1tch/gorepoman`
  is the sole distribution point for the default case;
  `github.com/ha1tch/repoman` remains the source for repositories still
  on the legacy Python vendored install (§0). If skill text and either
  repository's own README disagree, the README wins.
