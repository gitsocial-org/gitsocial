#!/usr/bin/env bash
# site-test.sh - run the S3 static-site read-surface test battery.
#
# Builds the showcase fixture (if absent), serves it on an ephemeral port, and
# runs the default battery of DOM-free unit suites plus fixture-backed route
# suites. Exits nonzero on any failure. The legacy tier (original frozen
# buckets) runs only when GS_SITE_LEGACY_ORIGIN points at a server hosting them.
#
# Usage:
#   scripts/site-test.sh
#   GS_SITE_LEGACY_ORIGIN=http://localhost:8000 scripts/site-test.sh
set -o pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec node "$root/library/core/objstore/sitetest/runner.js"
