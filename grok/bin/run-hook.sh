#!/usr/bin/env bash
# Back-compat wrapper — prefer grok/bin/run-hook.
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/run-hook" "$@"
