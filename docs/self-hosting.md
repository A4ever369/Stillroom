# Self-hosting `stillroomd`

`stillroomd` gives a team one search box over the knowledge every repo has
distilled. It is a single Go binary with **no database**.

## The one design decision everything follows from

> **The server owns no source of truth.**

Every document it serves is derived from a `.team-context/` directory that
lives in a git repo your team already owns. The index is a cache.

That buys the things that normally make an internal tool hard to get approved:

| | Because the server holds only derived data |
| --- | --- |
| Deployment | One container. No Postgres, no migrations, no queue. |
| Backups | None to take. Delete the volume and it rebuilds from git. |
| Security review | A compromise of this service exposes nothing that was not already in your git host. |
| Exit | Stop the container. Not one byte of knowledge is lost — it was never here. |

Two invariants the server is built never to break, and which the test suite
enforces:

- **It never reads the evidence plane.** Session transcripts stay on the
  machine that produced them. Only distilled, reviewed, committed knowledge is
  served. (`.team-context/queue/` is machine-private and is not indexed.)
- **It never writes to a repo.** Knowledge changes ride pull requests, in the
  tool your team already reviews with.

## Run it

```bash
# from source
make stillroomd
./bin/stillroomd -scan ~/code

# docker: mount your checkouts read-only
docker build -t stillroomd .
docker run --rm -p 8080:8080 -v /srv/checkouts:/checkouts:ro stillroomd -scan /checkouts
```

Then open <http://localhost:8080>.

### Flags

| Flag | Meaning |
| --- | --- |
| `-scan DIR` | index every repo under `DIR` that has a `.team-context/` (repeatable) |
| `-repo NAME=PATH` | index one repo under an explicit display name (repeatable) |
| `-addr :8080` | listen address |
| `-refresh 1m` | how often to rebuild the index from disk |
| `-version` | print the version and exit |

`-scan` skips `node_modules`, `vendor`, `target`, `dist`, `build`, `.venv` and
dot-directories, and stops descending once it finds a knowledge base — so
pointing it at a whole `~/code` tree is fine.

## Keeping the checkouts fresh

The server reads whatever is on disk and re-indexes every `-refresh`. Keeping
the checkouts current is deliberately *not* its job — use whatever your
environment already trusts:

```bash
# a cron entry next to the container is enough for most teams
*/5 * * * * cd /srv/checkouts/infra && git pull --ff-only --quiet
```

Only `.team-context/` is ever read, so a sparse checkout keeps the disk cost
near zero:

```bash
git clone --filter=blob:none --sparse git@github.com:acme/infra.git
cd infra && git sparse-checkout set .team-context
```

## Permissions

There is deliberately **no permission model of its own** yet. The intended
deployment is:

- put it behind whatever your org already uses (SSO proxy, VPN, internal
  network), and
- only mount checkouts of repos that everyone who can reach the service is
  already allowed to read.

The planned model is to delegate rather than invent one — *if you can read the
repo, you can read the repo's knowledge* — by asking the git host. Building a
second, divergent copy of your repo permissions would be a liability, not a
feature.

## The JSON API

The same ranking the UI shows, for CI jobs and agents:

```bash
curl -s 'http://localhost:8080/api/search?q=postgres+image&kind=fact' | jq '.results[0]'
```

Parameters: `q`, `repo`, `kind` (`fact`|`playbook`), `status`, `stale` (days).
`/healthz` reports version, repo count, document count and index build time.

## What this is not (yet)

- **No activity or per-person views.** Those need consent to be designed first,
  not retrofitted — a view of "what a teammate did today" that the person did
  not publish themselves is surveillance, and it would poison adoption of
  everything else. It comes after `still standup`, which puts the person in
  control of their own summary.
- **No writes.** Editing knowledge here would route around the PR review that
  makes the knowledge trustworthy.
- **No cross-repo conflict detection yet.** Two teams holding contradictory
  facts is the most valuable thing this index could surface, and it is the next
  thing to build.
