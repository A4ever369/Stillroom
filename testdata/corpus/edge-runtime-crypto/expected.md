# Expected knowledge — edge-runtime-crypto

Topic-level; not exact wording.

## Should learn (recall)

- In this app (Next.js 16), `proxy.ts` is the renamed middleware and runs on the
  Edge runtime, which has no `node:crypto`. License/entitlement verification
  (Ed25519) therefore cannot run in `proxy.ts`; it must be enforced inside Node
  route handlers (`app/api/*/route.ts`). Keep the edge proxy to cheap cookie /
  redirect work.
- The repo is pnpm-managed: `npm install` fails on the `node_modules/.pnpm`
  symlink layout. Use `pnpm install --frozen-lockfile`.

## Should NOT do (precision)

- Do not restate the exact build-output line or the grep command as a fact.
- Do not invent a general "Edge runtime" tutorial; the fact is the concrete
  constraint in THIS project and where the check moved to.

## Granularity

- Two facts (the edge-runtime crypto constraint, and pnpm-only). Both are
  durable gotchas a new teammate would otherwise rediscover the hard way.
