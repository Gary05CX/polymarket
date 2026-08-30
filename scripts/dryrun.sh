#!/bin/sh
set -e
cd "$(dirname "$0")/.."
if [ ! -f config.yaml ]; then
  cp config.example.yaml config.yaml
fi
go run ./cmd/bot -config config.yaml -mode dry_run -max-windows 1
