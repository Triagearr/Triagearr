#!/usr/bin/env bash
# Rebuild the daemon and restart it through process-compose — but only if the
# build succeeds. Invoked by the `watch` process (watchexec) on .go changes;
# `set -e` is the build gate: a failed compile aborts before the restart, so
# the last-good daemon keeps running with the error visible in the watch log.
#
# Atomic write (.new + mv) avoids ETXTBSY against the running binary.
# PC_SOCKET_PATH is inherited from scripts/dev.sh so the CLI hits this instance.
set -euo pipefail
cd "$(dirname "$0")/.."

go build -o .dev/triagearrd.new ./cmd/triagearr
mv -f .dev/triagearrd.new .dev/triagearrd
process-compose -U process restart daemon
