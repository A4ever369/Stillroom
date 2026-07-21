# Expected knowledge — example-ci-pg-image

Topic-level; not exact fact wording.

## Should learn (recall)

- CI's Postgres service image must be `pgvector/pgvector:pg16`, not plain
  `postgres:16` — the migrations use the `vector` extension, and the stock
  image lacks it (fails with `extension "vector" is not available`, SQLSTATE
  0A000). Local, CI, and prod should all use the same pgvector image.
- Deploy-order gotcha: migrations (including `CREATE EXTENSION vector`) must
  finish on the target database BEFORE traffic is cut to the new version,
  or the new code hits 0A000 on its first query against a vector column.

## Should NOT do (precision)

- Do not emit generic Postgres/CI tutorial facts ("use services in GitHub
  Actions", "what pgvector is").
- Do not restate the literal diff line as a fact; the durable knowledge is
  the *constraint* (images must match, migrations before cutover), not the
  one-line edit.

## Granularity

- Two facts is about right: one for the CI image constraint, one for the
  deploy ordering. A single merged blob is too coarse; splitting each into
  three trivia facts is too fine.
