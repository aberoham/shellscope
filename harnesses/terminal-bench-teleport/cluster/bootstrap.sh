#!/usr/bin/env bash
# First-time setup of the Teleport test cluster:
#   - tbench-agent role (login=agent, full node access for fixtures)
#   - agent-user user
#   - long-lived node-join token, written to .node-join-token
#   - signed identity file for the controller, written to .agent-identity
#
# Re-runnable: tctl create -f overwrites; token regen produces a new one.
set -euo pipefail

cluster_dir="$(cd "$(dirname "$0")" && pwd)"
cd "$cluster_dir"

tctl() { docker compose exec -T teleport-auth tctl "$@"; }

tctl create -f - <<'YAML'
kind: role
version: v7
metadata:
  name: tbench-agent
spec:
  allow:
    logins: [agent, root]
    node_labels:
      "*": "*"
    rules:
      - resources: [session, node]
        verbs: [list, read]
YAML

tctl create -f - <<'YAML'
kind: user
version: v2
metadata:
  name: agent-user
spec:
  roles: [tbench-agent]
  traits:
    logins: [agent, root]
YAML

# Mint a node-join token (TTL 7 days). Tokens are write-once; we issue a
# fresh one on every bootstrap. v17 `--format=text` prints the bare
# token string, one per line; we take the first non-empty line.
TOKEN="$(tctl tokens add --type=node --ttl=168h --format=text \
          | tr -d '\r' | awk 'NF { print; exit }')"
if [[ -z "$TOKEN" ]]; then
  echo "ERROR: failed to mint node-join token" >&2
  exit 1
fi
echo "$TOKEN" > .node-join-token
chmod 600 .node-join-token
echo "Wrote .node-join-token"

# Mint an identity file for the controller. tsh --identity uses this in
# place of an interactive login, which is what the runner needs.
# Note: distroless image has no shell, but tctl is a static binary that
# `docker exec` invokes directly — no `bash -lc` wrapper needed.
docker compose exec -T teleport-auth tctl auth sign \
  --user=agent-user \
  --out=/var/lib/teleport/agent-identity \
  --format=file \
  --ttl=168h \
  --proxy=localhost:3080 \
  --overwrite
docker compose cp teleport-auth:/var/lib/teleport/agent-identity .agent-identity
chmod 600 .agent-identity
echo "Wrote .agent-identity"

echo
echo "Cluster bootstrapped."
echo "  proxy:           localhost:3080"
echo "  cluster name:    tbench-fixture"
echo "  agent identity:  cluster/.agent-identity"
echo "  node-join token: cluster/.node-join-token"
