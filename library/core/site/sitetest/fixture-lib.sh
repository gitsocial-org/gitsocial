#!/usr/bin/env bash
# fixture-lib.sh - the locals3 server and isolated state both fixture builders share.
# Sourced by fixture.sh and shapes.sh, which set `here`, `repo`, `out` and
# `served` first and keep their own stamp, reuse guard and buckets.

# The s3 identity host is the RFC-2606 placeholder; GITSOCIAL_S3_ENDPOINT
# redirects the traffic to the locals3 server on 127.0.0.1.
HOST=fake.example.com

# fixture_stamp hashes the sources a fixture's served bytes come from: the site
# assets, the site generators, this file and the builder script named in $1.
# Reusing a fixture is only sound while that stamp is unchanged.
fixture_stamp() {
	(
		cd "$repo/library/core/site"
		find assets -type f | sort | xargs git hash-object
		find . -maxdepth 1 -name 'site_*.go' ! -name '*_test.go' | sort | xargs git hash-object
		git hash-object "$here/fixture-lib.sh" "$1"
	) | git hash-object --stdin
}

# fixture_build_bins builds the CLI and the locals3 helper, and points the config
# and cache at $out. The binary is always rebuilt, since only one built from the
# stamped sources makes the stamp true.
fixture_build_bins() {
	bin="$repo/bin/gitsocial"
	echo "building bin/gitsocial ..."
	(cd "$repo" && go build -o bin/gitsocial ./cli/gitsocial)
	# Plain `git push` resolves the s3 remote through the repo-local alias
	# `!gitsocial __git-remote-s3`, which looks the binary up in PATH: without
	# this an installed gitsocial would serve the fixture's push surface.
	export PATH="$repo/bin:$PATH"
	locals3bin="$out/locals3bin"
	go build -o "$locals3bin" "$here/../../objstore/locals3"
	export XDG_CONFIG_HOME="$out/xdg"
	cache="$out/cache"
}

# fixture_start_locals3 serves $served on an ephemeral port, sets $port and
# exports the s3 environment every push in the build then resolves through.
fixture_start_locals3() {
	"$locals3bin" -addr 127.0.0.1:0 -root "$served" >"$out/locals3.log" 2>&1 &
	s3pid=$!
	trap 'kill $s3pid 2>/dev/null || true' EXIT
	port=""
	for _ in $(seq 1 50); do
		port=$(sed -nE 's#.*127\.0\.0\.1:([0-9]+).*#\1#p' "$out/locals3.log" | head -1)
		[ -n "$port" ] && break
		sleep 0.1
	done
	[ -n "$port" ] || { echo "locals3 did not start" >&2; cat "$out/locals3.log" >&2; exit 1; }
	export GITSOCIAL_S3_ENDPOINT="http://127.0.0.1:$port"
	export GITSOCIAL_S3_PATH_STYLE=1
	export GITSOCIAL_S3_ACCESS_KEY=dummy
	export GITSOCIAL_S3_SECRET_KEY=dummy
	export GITSOCIAL_S3_REGION=us-east-1
}

# ident switches the workspace git author identity.
ident() { git -C "$W" config user.name "$1"; git -C "$W" config user.email "$2"; }
