#!/usr/bin/env bash
# Regenerates the README screenshots in docs/screenshots/ from the VHS tapes in docs/tapes/.
#
#   make screenshots                    # all tapes
#   bash docs/screenshots.sh form       # only docs/tapes/form.tape
#
# Needs go, vhs (brew install vhs; pulls ttyd and ffmpeg) and optionally pngquant/oxipng.
#
# Everything runs in a throwaway sandbox: its own HOME with a copy of the demo config docs/demo/mcp.json,
# and docs/demo/fake1mcp.go in place of the 1mcp CLI and service (status, tools). No real 1MCP, MCP server,
# account or token is involved, and your own ~/.config/1mcp is never read.
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"

sandbox=$(mktemp -d /tmp/lazy1mcp-demo.XXXXXX)
fake_pid=
cleanup() {
	[ -n "$fake_pid" ] && kill "$fake_pid" 2>/dev/null || true
	rm -rf "$sandbox"
}
trap cleanup EXIT

demo_home="$sandbox/home"
mkdir -p "$demo_home/.config/1mcp" "$sandbox/bin"
go build -trimpath -o "$sandbox/bin/lazy1mcp" .
go build -trimpath -o "$sandbox/bin/1mcp" docs/demo/fake1mcp.go

port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')
export ONE_MCP_CONFIG="$demo_home/.config/1mcp/mcp.json"
export LAZY1MCP_URL="http://127.0.0.1:$port" LAZY1MCP_BIN="$sandbox/bin/1mcp" LAZY1MCP_DEMO_HOME="$demo_home"
export PATH="$sandbox/bin:$PATH"
unset VISUAL EDITOR
"$sandbox/bin/1mcp" serve --port "$port" &
fake_pid=$!
for _ in $(seq 50); do
	curl -fs "$LAZY1MCP_URL/health" >/dev/null && break
	sleep 0.1
done

tapes=("$@")
if [ ${#tapes[@]} -eq 0 ]; then
	for f in docs/tapes/*.tape; do
		[ "$(basename "$f")" = config.tape ] || tapes+=("$(basename "$f" .tape)")
	done
fi
for t in "${tapes[@]}"; do
	echo "== $t"
	cp docs/demo/mcp.json "$ONE_MCP_CONFIG" # every tape starts from the same config
	vhs "docs/tapes/$t.tape"
done
[ -n "${KEEP_VHS:-}" ] || rm -rf .vhs

cd docs/screenshots
command -v pngquant >/dev/null && pngquant --force --skip-if-larger --quality 80-95 --ext .png ./*.png || true
command -v oxipng >/dev/null && oxipng -q -o 4 --strip safe ./*.png || true
ls -lh
