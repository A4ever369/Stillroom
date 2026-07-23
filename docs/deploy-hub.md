# Running the hub (stillroom.sh)

`stillroom-hub` is the service behind share links. Someone runs `still publish`,
gets a URL, and sends it to a colleague who pastes it into their own agent.

It is one static binary and a directory. There is no database.

## What it holds, and what that obliges you to do

This is the one component in the project that **owns a source of truth**.
`stillroomd` indexes git checkouts and can be rebuilt from scratch; the hub's
`/data` directory is the only copy of a pack once the publisher's machine has
moved on.

That makes the operational rules short:

- **Back up `/data`.** It is a directory of JSON files; any file-level backup
  works. Losing it breaks every link anyone has ever sent.
- **Do not put anything else in it.** Packs are immutable and content-addressed,
  so the directory is safe to copy, rsync or snapshot while running.
- **Everything in it was uploaded deliberately**, item by item, by someone who
  was shown exactly what would leave their machine. Nothing here was collected
  in the background, and it must stay that way.

## Run it

```bash
docker build -f Dockerfile.hub -t stillroom-hub .

docker run -d --name stillroom-hub -p 8080:8080 \
  -v stillroom-packs:/data \
  stillroom-hub -base-url https://stillroom.sh
```

Put TLS in front of it (Caddy, nginx, a load balancer — whatever you already
run). The hub speaks plain HTTP and does not terminate TLS itself.

`-base-url` is not cosmetic: it is the origin baked into every link the service
hands out and into the OAuth redirect. Set it to the public address, not
whatever the process binds to.

### Flags and environment

| | |
| --- | --- |
| `-addr :8080` | listen address |
| `-data /data` | where packs live |
| `-base-url` | public origin; appears in every share link |
| `-install-hint` | the install line shown on the pages; defaults to `curl -fsSL <base>/install.sh \| sh` |
| `GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET` | enable GitHub sign-in (optional) |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | enable Google sign-in (optional) |

## Sign-in is optional

Without OAuth credentials the hub runs in **anonymous mode**: publishing works,
links work, and packs simply carry no publisher. The page and the receiving CLI
both say so.

This is a deliberate default rather than an unfinished feature. Attribution
matters less here than it first appears — the link reached its recipient
through Slack, or email, or a message from someone they know, so they already
know who sent it. **Trust comes from the channel, not from the hub.** And
revocation, the thing people actually need, does not require an account:
publishing hands back a one-time token that `still revoke <link>` uses.

Turn sign-in on when you want:

- packs attributed to a name on the pack page,
- a `/me` list of what you have published, with download counts,
- revocation from a machine other than the one that published.

Two providers are supported, and either alone is enough — the audience splits
cleanly, developers have GitHub and everyone has Google. Configure whichever you
want; with both, the visitor picks.

| Provider | Register at | Callback URL |
| --- | --- | --- |
| GitHub | github.com/settings/developers | `https://stillroom.sh/auth/callback/github` |
| Google | console.cloud.google.com → Credentials → OAuth client ID (Web) | `https://stillroom.sh/auth/callback/google` |

Put the credentials in `/etc/stillroom-hub.env` (mode 0600) and restart. The hub
uses the provider's access token once, to read a display name and avatar, then
discards it — it never stores a credential that can act on someone's account
elsewhere, and it never sees or handles a password. The CLI signs in through the
device flow (`still auth login`), so a terminal never prompts for one either.

Account ids are namespaced by provider, so a GitHub id and a Google id are
never confused for the same person.

**With sign-in enabled, anonymous publishing is refused.** That is the point:
an open upload endpoint on a public host is somebody else's free storage.

## Before pointing the domain at it

1. **Cut a release first.** `install.sh` downloads from GitHub Releases. Until a
   tag exists it fails with a message pointing at `go install` — honest, but not
   what you want the first visitor to see. Order: tag → deploy → announce.
2. **Check the install line renders correctly** at `https://stillroom.sh/` — it
   is derived from `-base-url`, so a wrong base URL produces a copy button that
   hands people a broken command.
3. **Decide on sign-in before launch, not after.** Turning it on later locks out
   everyone who published anonymously from re-publishing, because anonymous
   uploads stop being accepted.

## Health and observability

`GET /healthz` returns version and liveness. Request logging goes to stdout, one
line per request, with no pack contents in it.

There is deliberately no analytics, no tracking and no third-party script on any
page: the whole product is asking people to trust it with something they wrote,
and a tracker on that page would be an odd way to ask.
