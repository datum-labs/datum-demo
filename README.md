# demo.datum.net

A public, split-screen demo showing the **same packet** traversing the
public internet ("without Datum") versus the Datum backbone ("with Datum").
Visitors see live latency / loss / throughput numbers side-by-side; the
Datum side comes out faster because the path is shorter and the link is
optimised.

The product brief lives in [`spec/01-from-jacob.md`](spec/01-from-jacob.md).
Deep notes on the surrounding Datum platform code (the tunnel client and
the Datum Cloud CLI) live in [`context/`](context/).

---

## What this MVP ships

Three statically-linked Linux binaries, two cloud servers, and one static
web page:

```
                       ┌──────────────────────────────────┐
                       │      demo.datum.net (page)       │
                       │   ┌──────────┬────────────────┐  │
                       │   │  without │      with      │  │
                       │   │   Datum  │      Datum     │  │
                       │   └──────────┴────────────────┘  │
                       └──────────────┬───────────────────┘
                                      │ polls /api/metrics every 2s
                                      ▼
                    ┌─────────────────────────────────────┐
                    │  Hetzner prober box (nbg1, default) │
                    │  ─ demo prober + UI                 │
                    │  ─ sqlite ringbuffer, 5-min window  │
                    │  ─ datumctl (ad-hoc debugging)      │
                    └─────────────────┬───────────────────┘
                                      │
                  ┌───────────────────┴───────────────────┐
                  │ direct                                │ datum
                  │ (public internet)                     │ (your Datum tunnel)
                  ▼                                       ▼
                                ┌──────────────────────────────────────┐
                                │  Hetzner origin box (ash, default)   │
                                │  ─ demo origin (fixed-size JSON)     │
                                │  ─ datum-connect (PR #130, static)   │
                                │  ─ datumctl                          │
                                └──────────────────────────────────────┘
```

The prober and origin are placed in **different Hetzner regions on
purpose** (default `nbg1 → ash`) so the "direct" path actually crosses
the public internet rather than hairpinning through Hetzner's
intra-datacentre fabric.

---

## Prerequisites

- Nix with flakes (`nix --version` ≥ 2.18, `experimental-features =
  nix-command flakes`)
- Terraform ≥ 1.6
- A Hetzner Cloud API token in `HCLOUD_TOKEN`
- An SSH key already uploaded to your Hetzner project — pass its name as
  `ssh_key_name`
- A Datum Cloud project — needed only when you wire up the "with Datum"
  side at the end

---

## Quick start

```bash
# 1. Provision both boxes (~3 minutes)
cd terraform
terraform init
terraform apply \
  -var=ssh_key_name=your-hetzner-key-name \
  -var=target_datum_url=https://your-datum-hostname/

# 2. Build + ship all three binaries to both boxes
cd ..
./deploy.sh                     # reads tf outputs, ships demo + datumctl
                                #   to both; datum-connect to origin only

# 3. Authenticate as the demo user on the origin box. The wrappers run
#    inside dbus-run-session with an unlocked empty-password gnome-keyring
#    so datumctl can store/read tokens on a headless box.
ssh root@$(cd terraform && terraform output -raw origin_ipv4)
datumctl-auth auth login              # browser device-code flow (datumctl)
datum-auth projects                   # pick the right project
datum-auth tunnel list                # sanity-check

# 4. Start the tunnel
systemctl enable --now datum-connect.service

# 5. Open the UI
xdg-open "$(cd terraform && terraform output -raw ui_url)"
```

---

## What's in the box

```
cmd/demo/                main.go — dispatches to `prober` and `origin`
internal/
  prober/                probe loop, sqlite store, /api/metrics
  origin/                tiny fixed-JSON HTTP server
web/                     embedded UI (vanilla HTML + CSS + JS, SVG sparklines)

terraform/               2 Hetzner boxes, 2 firewalls, 1 network
                         (SSH key is looked up by name, not uploaded)
  main.tf
  variables.tf
  outputs.tf
  cloud-init-prober.yaml   demo user, demo-prober.service, optional Caddy
  cloud-init-origin.yaml   demo user, demo-origin.service, datum-connect.service (disabled)

flake.nix                devshell + 3 statically-linked packages:
                           .#demo            (our prober + origin)
                           .#datumctl        (CGO=0 override of upstream)
                           .#datum-connect   (pkgsStatic + musl + static OpenSSL,
                                              built from app PR #130)
deploy.sh                builds all three, ships to the right boxes,
                         restarts the right systemd units

context/                 reference notes on ../app and ../datumctl
                         (snapshot from 2026-05-12; verify against live repos
                          before acting on specifics)
spec/                    the product brief
CLAUDE.md                context for AI assistants working in this repo
```

---

## Knobs worth knowing

All of these are Terraform variables. Defaults in **bold**.

| Variable | Default | Notes |
|---|---|---|
| `location` | **`nbg1`** | Prober region (Hetzner: `nbg1` `fsn1` `hel1` `ash` `hil` `sin`) |
| `origin_location` | **`ash`** | Origin region. Pick something distant from `location` |
| `server_type` / `origin_server_type` | **`cpx21`** | Hetzner type (3 vCPU AMD, 4 GB, ~€7.55/mo). Cheapest AMD type still orderable everywhere — `cpx11` was retired. `cx22` is EU-only and `cax11` (ARM) needs arm64 binaries |
| `target_direct_url` | auto | Auto-wires to origin's public IP; override for a third-party origin |
| `target_datum_url` | **required** | The Datum-fronted URL the prober hits for the "with Datum" path |
| `ssh_key_name` | **required** | Name of an existing SSH key in your Hetzner project |
| `demo_hostname` | `""` | If set, Caddy auto-provisions Let's Encrypt and fronts the prober |
| `ssh_source_ips` | `0.0.0.0/0` | Narrow this once you know your IP |
| `origin_payload_bytes` | `4096` | Response body size — bigger = throughput, smaller = latency dominant |

---

## Wiring the Datum side

`./deploy.sh` puts `datum-connect` on the origin and drops a disabled
`datum-connect.service`. To turn the "with Datum" path on:

```bash
# SSH in
ssh root@<origin-ipv4>

# datum-auth wraps `datum-connect` with the right HOME / XDG_DATA_HOME /
# DATUM_CONNECT_REPO so auth state lands where the systemd unit expects it.
datum-auth auth login --no-browser    # paste URL into a browser to finish
datum-auth projects                   # pick the project
datum-auth tunnel listen \
  --endpoint 127.0.0.1:8080 \
  --label demo-origin \
  --yes                               # one-shot sanity check
# ^C once you see "ready"

systemctl enable --now datum-connect.service
systemctl status datum-connect.service
```

Then in your Datum project, create an `HTTPProxy` that points its
hostname at the connector you just registered. The public hostname goes
into `target_datum_url` (`terraform apply -var=target_datum_url=...`) so
the prober knows where to probe.

See [`context/04-datum-cloud-control-plane.md`](context/04-datum-cloud-control-plane.md)
for the resource shapes.

---

## What's deliberately out of scope (today)

- **macOS desktop app, tray UI, signed installers.** The brief calls for
  these in week 1; we're prioritising the page + headless story first.
- **"Your own packet" experience.** Visitors see the prober's numbers,
  not their own. The spec's headline aha softens — closer to "look at
  this dashboard" than "watch your packet move." Easy to add back later
  by shipping the prober binary as a visitor download.
- **OTel / WAF / audit / team mode / inference stub.** Weeks 2–3 of the
  brief.
- **Multi-vantage-point probing.** Single prober, single perspective.
  Easy to fix by running the same binary on 2–3 cheap VMs and
  aggregating.
- **TLS auto-provisioning when `demo_hostname` is unset.** Plain HTTP on
  `:8080` unless you point a hostname at it and let Caddy handle the
  rest.

---

## Build gotchas (one-pager)

- **First `nix build .#datum-connect` takes ~8 min** — full Rust
  workspace compile with musl. Warm cache makes it instant after that.
- **`flake.nix` pins app PR #130 (`cli-tunnel-and-auth`)** via
  `git+file:///home/gianarb/git/datum/app?ref=refs/remotes/origin/cli-tunnel-and-auth`.
  If you move this repo to another machine, the `app-src` URL needs
  updating. `nix flake update app-src` pulls fresh from the branch.
- **`iroh-proxy-utils-0.1.0`'s git hash is locally overridden** because
  the upstream PR #130 flake has a stale placeholder for it. If
  `iroh-proxy-utils`'s upstream commit moves, the build will fail with a
  hash mismatch — paste the new hash from the error into `flake.nix`.
- **The origin is in a different region from the prober on purpose.**
  Putting both in `nbg1` will hairpin the "direct" path through
  Hetzner's network and the comparison loses its punch.
- **Don't `terraform destroy` and forget the Datum side.** The
  `HTTPProxy` + `Connector` in your Datum project outlive the Hetzner
  box; clean them up with `datumctl delete ...` separately.

---

## See also

  — what actually happens end-to-end inside a Datum tunnel
- [`CLAUDE.md`](CLAUDE.md) — guidance for AI assistants working in this
  repo
