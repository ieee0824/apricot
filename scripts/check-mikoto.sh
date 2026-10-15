#!/usr/bin/env bash
set -eu

exec "${MIKOTO_BIN:-mikoto}" -max-lines=80 ./...
