# CLAUDE.md

## What this repo is

This is the working repo for building the public **`demo.datum.net`** site —
a three-week project to ship a live demo of Datum Cloud's tunneling, DNS,
WAF, observability, and audit story for platform engineers at Alt Clouds
(Baseten, Supabase, Mistral).

**Always read it before doing substantive planning work** — it pins the
audience, the comparator ("Without Datum" = public internet), the success
criteria, and the open decisions still on the table.

This repo is **not** yet a git repository. Don't `git init` without asking.

## Where the platform code lives

The demo composes work from sibling repos:

- `../app` — Datum Connect: the Rust tunnel client (desktop GUI + headless
  CLI). The tunnel daemon, cert identity, and heartbeat agent the demo brief
  calls out **already exist here**.
- `../datumctl` — the Datum Cloud CLI. The "every component is a manifest
  applied with `datumctl`" story leans on this.

## Working preferences

- Don't introduce frameworks, build systems, or scaffolding before there's a
  concrete need. The repo is empty for a reason; the brief favours
  incremental beats over upfront architecture.
- The demo brief lists open decisions (Go vs Rust daemon, US-only map vs
  global, etc.) and explicit leans. Respect the lean unless the user
  explicitly reopens the decision.
- If asked to plan or implement something that already exists in `../app` or
  `../datumctl`, **say so** and link to the existing code instead of
  re-implementing. The demo is meant to expose existing platform pieces, not
  fork them.
- "Without Datum" in the comparator is the public internet — never name a
  competitor vendor.
- `demo.datum.net` is the eventual public URL. Don't hard-code production
  URLs into local dev configs.

## When you need facts from GitHub

`gh` is authenticated and works against both repos. Quick recipes:

```bash
gh issue list --repo datum-cloud/app --state open --limit 20
gh pr   list --repo datum-cloud/datumctl --state merged --limit 20 \
  --json number,title,mergedAt --jq '.[] | "#\(.number) \(.mergedAt[:10]) \(.title)"'
gh issue view <N> --repo datum-cloud/app --json title,body --jq '.body'
gh release view <tag> --repo datum-cloud/datumctl --json body --jq '.body'
```

## Things you should know that aren't obvious from the file tree

- `../app` was renamed from `datum-connect`. Release artefacts and the
  Homebrew tap still use the old name (`datum-cloud/tap/desktop`,
  `datum-connect` Cargo bin).
- The **gateway** is no longer in `../app` — it moved to
  `datum-cloud/iroh-gateway` in app PR #145 (2026-04-15). Older issues that
  say "the gateway" are referring to code that lives elsewhere now.
- `../datumctl` is `kubectl` library code wrapped to hide Kubernetes
  vocabulary from end users. The CLI's own `CLAUDE.md` makes this rule
  explicit: never say "kubectl" or reference pods / deployments / services /
  nodes in user-facing text.
- The local clone at `../datumctl` has two remotes; `origin` is the
  ShippingBytes fork, `upstream` is `datum-cloud/datumctl`. Confirm with
  `git remote -v` before pushing.
- Datum identifies devices by **iroh endpoint id** (z32-encoded public key);
  it shows up as `status.connectionDetails.publicKey.id` on the `Connector`
  resource and as the field selector the heartbeat agent uses for lookups.
- Service accounts used to be called "machine accounts." The rename happened
  in datumctl v0.14.0 (PR #164). Old paths
  (`~/.config/datumctl/machine-accounts/`) no longer work.
- `--namespace` exists for kubectl compatibility but only `default` is
  supported in PCPs today.

## Don't

- Don't add documentation files (README.md, etc.) inside subdirectories
  unless explicitly asked. The user has objected to unprompted READMEs.
- Don't `git init` this repo without asking.
- Don't run `ultrareview`, `git push`, or anything else that creates external
  artefacts without explicit approval.
- Don't echo back the demo brief at the user; they wrote it.
