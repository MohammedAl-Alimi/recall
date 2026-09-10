#!/usr/bin/env sh
# Fails when the tree contains anything that looks like a secret or a
# machine-specific identifier. Runs in CI and before every push.
set -eu
cd "$(dirname "$0")/.."
status=0
pattern='(sk-ant-[A-Za-z0-9_-]{10,}|ghp_[A-Za-z0-9]{20,}|gho_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16}|xox[bp]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|Bearer [A-Za-z0-9._-]{30,}|/Users/[a-z][a-z0-9._-]*-[a-z][a-z0-9._-]*/)'
if grep -rEIn --exclude-dir=.git --exclude-dir=dist --exclude=go.sum --exclude=check-secrets.sh "$pattern" . ; then
  echo "check-secrets: possible secret or personal path found (see above)" >&2
  status=1
fi
# Fixture transcripts must be synthesized: refuse anything under testdata larger than 2 MB.
big=$(find testdata -type f -size +2M 2>/dev/null || true)
if [ -n "$big" ]; then
  echo "check-secrets: oversized fixture files (real transcripts are not allowed):" >&2
  echo "$big" >&2
  status=1
fi
exit $status
