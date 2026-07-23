# Stillroom — Product Design

[`design-v2.md`](design-v2.md) is the **architecture**: two planes, git as the
fusion algorithm, positioning, the competitive landscape, the server plane.
This document is the **product**: who uses it, what they touch, what the rules
are, how a team gets from zero to habit, and what this becomes over three years.

It exists because the project is crossing from *"a CLI that works"* to *"a thing
a team adopts"*, and the failure modes at that crossing — cold start, knowledge
rot, and the surveillance trap — are not architectural problems. None of them
get better by writing more Go.

Commercialization is deliberately out of scope here; it lives in
[`design-v2.md` §14](design-v2.md).

---

## 1. The product in one sentence

> **Team memory for AI engineering.**

Subject = the team. Benefit = memory. Domain = engineering.

It is worth naming what this is *not* called, because the wrong framing quietly
determines the wrong product:

- ❌ *"Git for AI traces."* Right as an architecture metaphor, wrong as a
  product. GitHub did not win by versioning files; it won because code is
  **reused and reviewed**. Traces are neither — nobody reads a colleague's
  session log, and nobody adopts version control for logs. A product built on
  storing traces is an observability tool, which is a smaller thing on someone
  else's turf.
- ❌ *"A wiki that writes itself."* Wikis fail because nobody maintains them.
  Automation makes an unmaintained wiki *bigger*, not better. See §5, L3.

The distinction that follows from this and shapes everything below:

| | Evidence | Knowledge |
| --- | --- | --- |
| What | session transcripts | facts, playbooks |
| Where | the machine that produced it | the team's git repo |
| Merged? | never | **that's the whole point** |
| Reviewed? | no | in the PR, like code |
| The product's atom? | no | **yes** |

**The commit-equivalent of this product is a knowledge change, not a trace.**
Everything downstream — review, search, health, activity — operates on knowledge.
Evidence is only ever cited.

---

## 2. Users and jobs

| Role | The job | The moment it hurts | Served by |
| --- | --- | --- | --- |
| **IC engineer** | Stop re-explaining this repo to my agent every session | Fourth time typing "we use pnpm, not npm" | invisible surface, CLI |
| **New joiner** | Be useful in week one | Day 3, still asking where the runtime config lives | invisible surface |
| **Knowledge maintainer** (rotating) | Keep the base from rotting | A stale fact sent someone to a decommissioned host | health surface (H1), PR bot |
| **Engineering leader** | Knowledge survives attrition; onboarding gets shorter | The one person who understood the deploy left | search, agent-subject reporting (H2) |
| **Security / platform reviewer** | Prove nothing sensitive leaves the building | The adoption request lands on their desk | privacy model, self-hosting doc |

### The tension that decides the product

**The user is not the buyer.** The IC wants their agent to stop being ignorant.
The leader wants retention, onboarding speed, and — increasingly — an answer to
*"what has AI actually been doing in our codebase?"*

The buyer's most natural ask is **visibility into people**. That ask is exactly
what destroys the user's trust, and a team tool that engineers distrust is dead
on arrival regardless of who signed for it. §6 exists to make that ask
un-buildable in its harmful form and answerable in a harmless one.

---

## 3. Object model — what users actually manipulate

| Object | Created by | Reviewed by | Lives in | Merged by | Visible to |
| --- | --- | --- | --- | --- | --- |
| **Fact** (`internal/ir`) | distiller, proposed | a human, in the PR | `.team-context/facts/<id>.md` | git, one file per fact | anyone who can read the repo |
| **Playbook** (`internal/ir`) | distiller, proposed | a human, in the PR | `.team-context/playbooks/` | LLM-assisted revision, committed | same |
| **Knowledge diff** (`internal/review`) | `still review` | the PR reviewer | a PR comment | n/a — it *is* the review | PR participants |
| **Digest** (`internal/session`) | adapters, in memory | nobody | never written to shared disk | never | **the producing machine only** |
| **Materialized context** | `still materialize` | nobody (generated) | `.team-context/materialized.md` | union merge, then re-rendered | every agent on the team |
| **Standup draft** (H2) | `still standup` | **its subject, before publishing** | local until published | never | whatever its author chose |
| **Health signal** (H1) | the server, derived | the maintainer | nowhere — recomputed | n/a | anyone who can read the repo |

Two properties of this table carry most of the design:

1. **One fact = one file.** Two people learning different things merge cleanly;
   two people learning *contradictory* things produce a git conflict — precisely
   the case a human should decide.
2. **Facts have time.** `observed_at` / `supersedes` / `status` mean a newer
   observation replaces an older one instead of piling up beside it. Without
   this, a knowledge base is a landfill. This is the machinery §5 L3 runs on.

---

## 4. The five surfaces

| Surface | Primary user | Purpose | Must never appear there |
| --- | --- | --- | --- |
| **Invisible** — plugin hook + `@.team-context/materialized.md` | everyone, unknowingly | knowledge arrives without anyone doing anything | anything requiring a decision |
| **`still` CLI** | contributor, maintainer | distill, inspect, repair | a token spend without an up-front count |
| **PR bot** — `still review` | reviewer | make a knowledge change reviewable as prose | anything that is not a knowledge change |
| **`stillroomd`** | the whole org | find knowledge outside the repo that produced it | evidence, person-subject views, any write path |
| **Agent API** — `/api/search`, later MCP | the agent itself | answer mid-task, not just at session start | unreviewed knowledge |

### 4.1 The invisible surface is the product

Roughly 90% of the value is delivered through a surface with **no UI at all**:
the hook enqueues, and `CLAUDE.md` imports `materialized.md`, so a teammate who
runs `git pull` gets the team's knowledge without knowing this product exists.

This is not a nice-to-have. Every competing approach in the wild
([design-v2 §12](design-v2.md)) fails at the same point: *it requires the user to
add an action*. The design constraint, restated as a product rule:

> **Zero new habits.** Every step must parasitize an action the team already
> performs. The user-visible surface converges to: install a plugin once, and
> read one extra section in a PR.

### 4.2 Why the CLI still has deliberate friction

`still distill` prints the pending count and what it will cost *before* running,
and `--dry-run` is the recommended first experience. That friction is not an
unfinished onboarding — it is the consent moment. A tool that silently spends
the user's tokens and writes to their repo the first time they touch it does not
get a second chance.

### 4.3 Why the review surface is a PR and not our own UI

Building a review UI would mean asking a team to review knowledge somewhere
other than where they already review things. The knowledge diff rides the same
PR as the code that taught it. Review is the human checkpoint that makes
everything else trustworthy (§6 R2, R4), so it must live where review already
happens.

### 4.4 The agent surface closes the loop

Today, knowledge reaches an agent at session start, statically. The end state is
an agent that *asks* — "has anyone on this team hit this error before?" — mid
task. `/api/search` already does this; MCP makes it native (H3). This is the
surface where the corpus stops being documentation and becomes infrastructure.

---

## 5. The three loops

### L1 — the compounding loop (this *is* the product)

```
session ends → digest (local) → distill → propose → review in PR → merge
    → materialize → every teammate's next session starts smarter → ↺
```

Everything else in this document supports L1. If L1 does not turn, no feature
saves the product; if L1 turns, the product improves whether or not anyone is
looking at it.

### L2 — the retrieval loop

Search (human) and query (agent) make knowledge useful **outside the repo that
produced it**. This is the entire justification for a server existing: within
one repo, `materialized.md` is enough.

L2 is also the only loop with **no political surface**. Nobody objects to
finding the pitfall another team already hit. That is why it shipped first
([design-v2 §17.4](design-v2.md)).

### L3 — the maintenance loop

```
staleness detected → re-observed? → yes: supersede · no: dispute → retire
```

**A knowledge base dies of rot, not of scarcity.** A stale fact is worse than a
missing one, because it is injected into every agent session with the authority
of team knowledge. Automation makes this worse, not better: it fills faster than
humans prune.

L3 is why `ir.Fact` carries `observed_at`, `supersedes` and `status` from the
first commit, and why the server's freshness facet is framed as *"old knowledge
is not wrong — it is unverified"*. H1 productizes this loop; until then the
rotating maintainer role does it by hand, and that is a real, assigned job.

---

## 6. The consent constitution

Five rules. They are invariants, not preferences — each is enforced by tests or
by an architectural impossibility, and a feature that requires breaking one does
not ship.

**R1 — Evidence never leaves the machine that produced it.**
Transcripts are digested locally and distilled through the user's own
`claude -p`. `.team-context/queue/` is machine-private and is never indexed
(`TestQueueIsNeverIndexed`). A fact's `source` field is a **citation, not a
link**.

**R2 — Nothing is shared without a human commit.**
There is no background upload and no auto-publish. The PR is the checkpoint.

**R3 — Any view whose subject is a *person* requires that person's publish
action. Views whose subject is an *agent* do not.**
This is the whole answer to the surveillance trap, and it is also the answer to
the legitimate compliance demand:

| | |
| --- | --- |
| ❌ A manager opens a report's page to see their day | employee surveillance — ends adoption permanently |
| ✅ A summary the person generated, edited, and chose to publish | asynchronous standup — people want it |
| ✅ *"41 agent sessions touched production config this week"* | agent-subject reporting — no consent needed, and it is what the buyer actually needs |

**R4 — Redaction scrubs credential *shapes*, not confidential *substance*.**
`internal/redact` runs twice (on the digest going in, on the distiller output
coming out) and catches things that look like keys, tokens and secrets. It does
**not** and cannot judge whether a sentence is commercially sensitive. The PR
review is the real checkpoint. No surface may imply otherwise — over-trusting
redaction is more dangerous than having none, because it removes the reader's
vigilance.

**R5 — The CLI keeps working with no server.**
The moment a core workflow requires a central service, the local-first trust
argument collapses and the product becomes a data-custody question.

### Anti-features

Written down so they are refused on sight rather than re-litigated:

- Manager drill-down into an individual's activity (R3)
- Leaderboards of fact counts, or any per-person contribution ranking — it makes
  volume the goal, and volume is exactly what poisons the base (§8)
- Silent auto-distill-and-push (R2)
- Server-side editing of knowledge — routes around the review that makes the
  knowledge trustworthy (§4.3)
- Storing transcripts centrally "just for search" (R1)

---

## 7. Onboarding and activation

### The cold-start problem is the number one killer

A team tool's value requires everyone, so nobody wants to be first. Most tools
in this category die here, not in the technology.

Stillroom's escape route is that **single-user value is already positive**: I
distill my own sessions, my own next session gets smarter. Team benefit is a
by-product that happens inside a PR I was opening anyway. That means adoption
can start with one person — which is the only way adoption ever starts.

The second half of the answer is seeding. A newcomer must never meet an empty
repo:

> Before anyone else installs anything, the champion distills the team's
> existing history into 30–50 real facts. A new teammate's day one is
> *"I did nothing, and my agent already knew why CI needs the pgvector image."*

An empty knowledge base asks every single person to be the first person. That is
the same failure, one level down.

### The funnel

| Step | What happens | Design intent |
| --- | --- | --- |
| Install | plugin once; `still init` | one action, ever |
| **See** | `still distill --dry-run` | look before you write — the consent moment (§4.2) |
| **Contribute** | first knowledge diff merged in a PR | knowledge becomes reviewable, not magic |
| **Activate** | 🎯 **a fact you did not write saves you** | the only moment that creates a believer |
| Habit | it happens without thinking | the invisible surface takes over |

**Activation is not installation.** The metric that matters is the fourth row,
and everything above it exists to make that row happen in week one. This is also
why seeding is not optional: without it, activation cannot occur until several
people have independently contributed, which is months.

### `still standup` as the on-ramp

For the skeptic who will not adopt a team tool, `still standup` (H2) offers pure
personal value — *distill what I did today across projects into my own draft* —
with no team dependency and no sharing unless they choose it. It is the widest
door into the product, and by construction it teaches R3 on first use.

---

## 8. Metrics

| Tier | Measure | Why |
| --- | --- | --- |
| Leading | distill runs/week; knowledge PRs merged; searches with a click-through; agent API calls | is the loop turning? |
| **Activation** | **% of members who consumed a fact they did not author** | the only number that proves the product works |
| Lagging | onboarding time to first meaningful PR; repeat-question rate in team chat; **supersession rate** | did it change how the team works? |

**Supersession rate deserves special attention**: facts being *corrected* is the
signal that the base is alive and being used. A base where nothing is ever
superseded is a base nobody trusts enough to argue with.

### Anti-metrics — never optimize these

- **Total fact count.** Trivially gamed, and the failure mode it produces
  (volume over durability) is exactly what makes a knowledge base useless.
- **Install count.** Measures curiosity, not value.
- **Sessions captured.** Capture is not knowledge.

### The qualitative tell

**Someone argues about a fact in a PR.** That is the moment the team has decided
the knowledge base is authoritative. No dashboard captures it, and it is worth
more than any of the numbers above.

---

## 9. The product landscape: today → three years

Each horizon is defined by the new **object** and **loop** it introduces — not by
a feature list — plus the risk it brings and the invariant it must not break.

### H0 — Repo knowledge *(shipped)*
- **Objects:** fact, playbook, knowledge diff, materialized context
- **Loops:** L1 complete; L2 via `stillroomd` search
- **Surfaces:** invisible, CLI, PR bot, org search, `/api/search`
- **Risk:** distillation quality. A first run full of noise loses the user
  permanently. Mitigation: the eval corpus and committed baseline (`make eval`).

### H1 — Knowledge health *(the retention horizon)*
- **New object:** health signal — staleness, cross-repo contradiction, orphaned
  knowledge, coverage gaps
- **New loop:** L3, productized. Weekly digest to the rotating maintainer;
  "these 6 facts have not been re-observed in 180 days"; **two repos asserting
  contradictory things** — the single highest-value thing a cross-repo index can
  surface, and nothing adjacent does it.
- **Why here:** rot is what kills knowledge bases at month six. H1 is what makes
  the product still useful in year two.
- **Invariant:** health is *derived and recomputed*, never a stored judgement —
  the server still owns no source of truth.

### H2 — Consent-gated personal layer *(the trust horizon)*
- **New objects:** standup draft (person-owned), published summary, agent-subject
  audit view
- **New loop:** publish-with-consent — the first loop where a person, not a
  repo, is the unit
- **Order is fixed:** `still standup` (local, my draft, I decide) ships
  **before** any team-wide activity view. Building the view first and retrofitting
  consent is how this feature becomes surveillance.
- **Risk:** the highest in the roadmap. R3 is the entire mitigation.

### H3 — Retrieval in the loop *(the ecosystem horizon)*
- **New objects:** MCP retrieval surface; promoted skill (fact/playbook → agent
  skill, human in the loop); shareable public playbook
- **New loop:** ask-mid-task — the agent queries team memory during work, not
  only at session start. This is where the corpus becomes infrastructure rather
  than documentation.
- **Also here:** distillation eval as a product surface (quality regressions
  visible to the team, not just to us); cross-org playbook sharing, which is the
  first genuinely networked effect.
- **Invariant:** only reviewed knowledge is ever retrievable. An agent must never
  be able to read an unreviewed proposal.

### What does not change across all four

The two-plane split, one-fact-one-file, review-in-the-PR, and the five rules of
§6. If a horizon requires changing one of those, the horizon is wrong.

---

## 10. Non-goals

- **Not a wiki.** No hand-authored pages, no page hierarchy, no WYSIWYG.
- **Not an observability product.** We do not chart, trace, or alert on agent
  runs. Traces are evidence, and evidence is not the product (§1).
- **Not a monitoring tool.** See R3.
- **Not a chatbot.** The interface to knowledge is the user's existing agent.
- **Not a model.** Distillation runs through the user's own tools, deliberately.

---

## 11. How it dies

| Cause of death | Mitigation | Where it lives |
| --- | --- | --- |
| **It becomes surveillance.** One manager drill-down and engineers stop trusting it; no second chance. | R3; standup before activity; agent-subject framing for the compliance ask | §6, H2 |
| **The base rots.** Stale facts get injected with team authority and actively make agents worse. | temporal facts from day one; L3; H1; a rotating maintainer whose only job is deleting | §5, §9 |
| **Distillation produces noise.** The user sees garbage once and never returns. | eval corpus + committed baseline; prompt changes gated on it | `eval/`, `docs/testing.md` |
| **A trace leaks.** Terminal for a product whose input is the company's sessions. | R1 is architectural, not procedural: there is no code path that uploads a transcript | §6 |
| **The platform ships it natively.** Anthropic or Cursor add team memory. | cross-tool neutrality is structurally unavailable to them; stay a neutral layer, never "a Claude Code plugin" | [design-v2 §11.3](design-v2.md) |

The first two are the likely ones. Both are product decisions, not engineering
ones, which is why they are written down here rather than left to judgement.
