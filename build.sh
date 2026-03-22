#!/bin/bash
# Delegates to build-go.sh — on feature/go-rewrite this is the canonical build script.
exec "$(dirname "$0")/build-go.sh" "$@"
