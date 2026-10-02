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
docume
  is the sole distribution point for the default case;
  `github.com/ha1tch/repoman` remains the source for repositories still
  on the legacy Python vendored install (§0). If skill text and either
  repository's own README disagree, the README wins.
