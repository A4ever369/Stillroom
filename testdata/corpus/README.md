# Eval corpus (L5 distillation-quality)

Each subdirectory is one graded case. `make eval` (see `cmd/eval`) runs every
case that has both required files:

```
testdata/corpus/<case-name>/
├── transcript.jsonl   a redacted real Claude Code session (the input)
└── expected.md        what a human says this session SHOULD teach (topic-level)
```

## transcript.jsonl

A real (or realistic) Claude Code transcript, **redacted of secrets first**.
Same JSONL shape the adapter already parses — one event per line, malformed
lines tolerated. Keep it long enough to clear `minTurns`, short enough to read.

## expected.md

Prose, topic-level — NOT the exact fact wording. Describe the durable knowledge
a teammate should walk away with, and (optionally) note traps the distiller
should NOT fall into (restating code, inventing config it never saw). The judge
compares the produced facts against this on recall / precision / granularity.

## Adding a case

1. Take a real session (`~/.claude/projects/**/**.jsonl`), copy it here, and
   run it through redaction — never commit raw credentials. Eyeball the diff.
2. Write `expected.md` from your own memory of the session.
3. `make eval` — then, once you trust the scores, freeze them:
   `cp eval/last-run.json eval/baseline.json` and commit. Future prompt changes
   diff against that baseline.

The `example-*` case shipped here is synthetic, only to exercise the harness
wiring; replace it with real sessions as they accumulate (M1).
