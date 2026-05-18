#!/usr/bin/env bash
# deploy.sh — build the demo + datum binaries and ship them to a box.
#
#   ./deploy.sh                            # reads terraform outputs; ships
#                                          # appropriate binaries to BOTH boxes
#   ./deploy.sh root@1.2.3.4 prober        # demo + datumctl
#   ./deploy.sh root@5.6.7.8 origin        # demo + datumctl + datum-connect
#
# Binaries installed to /usr/local/bin/ on the target:
#   - demo            (always)
#   - datumctl        (always, on both roles)
#   - datum-connect   (origin role only)
#
# Then the script restarts:
#   - role=prober → systemctl restart demo-prober.service
#   - role=origin → systemctl restart demo-origin.service
#                   (datum-connect.service is left alone — enable manually
#                    after you've authenticated; see cloud-init final_message)

set -euo pipefail

cd "$(dirname "$0")"

build() {
  local target="$1"
  echo ">> nix build .#$target" >&2
  nix --extra-experimental-features 'nix-command flakes' build ".#$target" --out-link "result-$target" >&2
  readlink -f "result-$target/bin"
}

ship_one() {
  local host="$1"; local bin_path="$2"; local remote_name="$3"
  echo ">>   scp $bin_path -> $host:/tmp/$remote_name"
  scp -q "$bin_path" "$host:/tmp/$remote_name"
  ssh "$host" "install -m 0755 /tmp/$remote_name /usr/local/bin/$remote_name && rm -f /tmp/$remote_name"
}

deploy_role() {
  local host="$1"; local role="$2"
  echo
  echo "=== $role @ $host ==="

  ship_one "$host" "$DEMO_BIN_DIR/demo"       "demo"
  ship_one "$host" "$DATUMCTL_BIN_DIR/datumctl" "datumctl"

  if [[ "$role" == "origin" ]]; then
    ship_one "$host" "$DATUM_CONNECT_BIN_DIR/datum-connect" "datum-connect"
  fi

  local service
  case "$role" in
    prober) service="demo-prober" ;;
    origin) service="demo-origin" ;;
    *) echo "unknown role: $role" >&2; exit 2 ;;
  esac

  echo ">>   systemctl daemon-reload + restart $service.service"
  ssh "$host" "systemctl daemon-reload && systemctl restart $service.service && sleep 1 && systemctl --no-pager status $service.service | head -15"
}

# ----------------------------------------------------------------------
# Build all three artifacts up front so failures surface immediately.
# ----------------------------------------------------------------------
DEMO_BIN_DIR=$(build demo-linux-amd64)
DATUMCTL_BIN_DIR=$(build datumctl)
DATUM_CONNECT_BIN_DIR=$(build datum-connect)
echo
echo ">> binaries staged:"
echo "   $DEMO_BIN_DIR/demo            ($(du -h "$DEMO_BIN_DIR/demo"            | cut -f1))"
echo "   $DATUMCTL_BIN_DIR/datumctl    ($(du -h "$DATUMCTL_BIN_DIR/datumctl"    | cut -f1))"
echo "   $DATUM_CONNECT_BIN_DIR/datum-connect ($(du -h "$DATUM_CONNECT_BIN_DIR/datum-connect" | cut -f1))"

# ----------------------------------------------------------------------
# Two modes: no-args reads terraform outputs and does both boxes; args
# mean explicit single-target deployment.
# ----------------------------------------------------------------------
if [[ $# -eq 0 ]]; then
  echo
  echo ">> no args — reading terraform outputs"
  if [[ ! -d terraform ]]; then
    echo "no terraform/ directory; pass <host> <role> explicitly" >&2
    exit 2
  fi
  pushd terraform >/dev/null
  ORIGIN_IP=$(terraform output -raw origin_ipv4)
  PROBER_IP=$(terraform output -raw prober_ipv4)
  popd >/dev/null

  deploy_role "root@${ORIGIN_IP}" "origin"
  deploy_role "root@${PROBER_IP}" "prober"
  echo
  echo ">> done."
  exit 0
fi

HOST="$1"
ROLE="${2:-prober}"
case "$ROLE" in
  prober|origin) ;;
  *) echo "role must be 'prober' or 'origin' (got: $ROLE)" >&2; exit 2 ;;
esac

deploy_role "$HOST" "$ROLE"
echo ">> done."
