#!/usr/bin/env bash
# site-test.sh - run the S3 static-site read-surface test battery.
#
# Builds the showcase fixture (if absent), serves it on an ephemeral port, and
# runs the battery of DOM-free unit suites plus fixture-backed route suites.
# Exits nonzero on any failure.
#
# Usage:
#   scripts/site-test.sh
set -o pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec node "$root/library/core/site/sitetest/runner.js"
