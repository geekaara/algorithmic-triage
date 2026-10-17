# Algorithmic Triage

**Your backlog is bigger than your team. This tool decides what your MVP doesn't need, and checks the rest fits before your deadline.**

It reads your Jira board, judges every backlog ticket against a short policy you write ([`mvp.md`](context/mvp.md)), and plans the MVP work into the sprints you have left. You see the plan first. Nothing changes in Jira until you approve it, and one command undoes it.

From the GDG DevFest Brisbane 2026 talk *Algorithmic Triage: Teaching AI to say no to your backlog*.

```
  Velocity   18 → 22 → 20  =  20 pts/sprint
  Backlog    29 tickets · 174 pts

  KEY       PTS  PRIORITY  VERDICT  PLACED       WHY
  TRIP-35    13  Highest   V2       V2 backlog   OpenAI sentiment analysis and recommendations are out of scope…
  TRIP-43     1  Low       MVP      Sprint 5     Privacy consent at sign-up is a legal non-negotiable…

  Result     6 MVP tickets · 15 pts · 23 to V2 · fits with 12 pts spare
```

## How it works

1. **You write the policy once.** `mvp.md` says what the MVP must prove (the *golden path*), what's out of scope, and what's non-negotiable.
2. **AI judges each ticket.** MVP, V2 (later), or split, and which line of your policy decides it.
3. **Plain code does the maths.** Velocity from your real sprints, dependencies, and what fits before the deadline. If the must-haves don't fit, it says *deadline at risk* instead of quietly dropping something.

The rule behind it: **AI for judgement, code for arithmetic.**

## Get started (about 10 minutes)

**You need:** [Go](https://go.dev/dl/) 1.23+, a Jira Cloud **Scrum** project with story points and future sprints, and these keys:

| Key | Where | Needed for |
|---|---|---|
| Atlassian API token | [id.atlassian.com → API tokens](https://id.atlassian.com/manage-profile/security/api-tokens). Use the plain **Create API token** button, not "with scopes" | Always |
| Gemini API key | [aistudio.google.com/apikey](https://aistudio.google.com/apikey) (free tier works) | Always |
| Vercel AI Gateway key | Vercel dashboard → AI Gateway → API Keys. Needs paid credits | Optional: the faster `combined` mode |

**1. Get the code**

```bash
git clone https://github.com/geekaara/algorithmic-triage.git
cd algorithmic-triage
go build -o bin/triage ./cmd/triage
```

**2. Add your settings**

```bash
cp .env.example .env
```

Open `.env` and fill in your Jira address, email, token, project key (e.g. `TRIP`) and Gemini key. `.env` stays on your machine and is never committed.

**3. Run it**

```bash
./bin/triage plan
```

## The three commands

| Command | What it does | Changes Jira? |
|---|---|---|
| `./bin/triage plan` | Shows the plan | No |
| `./bin/triage apply` | Moves MVP tickets into sprints, labels everything `mvp` or `v2`, and comments on each ticket with the reason | Yes |
| `./bin/triage reset -yes` | Undoes `apply` (run `reset` without `-yes` to preview) | Yes |

Re-runs are instant: answers are cached until a ticket or your policy changes.

## Two ways to judge

| | Command | Best for |
|---|---|---|
| **Gemini** (default) | `./bin/triage plan` | Getting started. Only needs a Gemini key |
| **Combined** | `./bin/triage plan -judge combined` then `./bin/triage apply -plan plan-combined.json` | Bigger backlogs. Faster and cheaper, same quality |

**Combined** uses each AI for what it's best at. **Jev**, a fast *decision model*, classifies every ticket in seconds and says how sure it is. **Gemini** writes the explanations, double-checks them, and makes the final call whenever Jev is unsure. On the demo board it matched Gemini's decisions on all 29 tickets, while Gemini only had to decide 6 of them.

## Write your own mvp.md

Copy [`context/mvp.md`](context/mvp.md) and keep its headings:

- **Golden path:** the numbered steps your users or investors must be able to do. A ticket is MVP only if a step breaks without it.
- **Non-negotiables:** legal, security and stability work you'll do regardless.
- **Out of scope:** what you're *not* building yet. One specific bullet each. This is where most of the cutting comes from.

Write it with your stakeholders, once. If the plan cuts something you need, **change the policy, not the ticket**, and run `plan` again.

## Try it on the demo board

`cmd/seed` creates the board from the talk (54 tickets, sprint history, dependencies) in an **empty, throwaway** company-managed Scrum project:

```bash
go run ./cmd/seed
./bin/triage plan
```

## If something goes wrong

| You see | Fix |
|---|---|
| `401` from Jira | Make a new token with the plain **Create API token** button, logged in with the same email as in `.env` |
| `429` or `503` from Gemini | Free-tier limits or a busy model. It retries automatically; wait a minute |
| `403 Free tier users do not have access` | `combined` needs paid Vercel AI Gateway credits, on the same team as the key |
| `Nothing to triage` | Everything is already triaged. Run `./bin/triage reset -yes` |

## Privacy

Ticket text and your `mvp.md` are sent to Google (Gemini), and for `combined` to Vercel and TypeSafe AI (Jev). No code, comments or names are sent. For company backlogs, use paid API keys and check with your security team.

## For developers

Go, standard library only, no dependencies. `go test ./...` runs the planner tests.

| Path | What |
|---|---|
| `cmd/triage` | The CLI: `plan`, `apply`, `reset` |
| `internal/triage` | Loading the board, the judges, and the planner |
| `internal/jira`, `internal/gemini`, `internal/typesafe` | API clients |
| `context/mvp.md` | Example policy |

More options: `./bin/triage plan -h`.
