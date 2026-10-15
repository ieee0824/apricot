#!/usr/bin/env bash
set -u

# Keep the README's 80-line rule for new functions. These existing functions
# are grandfathered at their current lengths, so they cannot grow unnoticed.
mikoto_bin=${MIKOTO_BIN:-mikoto}
output=$("$mikoto_bin" -max-lines=80 ./... 2>&1)
status=$?

if (( status != 0 && status != 3 )); then
  printf '%s\n' "$output" >&2
  exit "$status"
fi

failed=0
while IFS= read -r line; do
  [[ -z $line ]] && continue
  if [[ ! $line =~ ^(.+):([0-9]+):([0-9]+):\ function\ ([[:alnum:]_]+)\ is\ ([0-9]+)\ lines\ \(limit\ 80\)$ ]]; then
    printf 'Unexpected mikoto diagnostic: %s\n' "$line" >&2
    failed=1
    continue
  fi

  file=${BASH_REMATCH[1]}
  function=${BASH_REMATCH[4]}
  lines=${BASH_REMATCH[5]}
  baseline=0
  case "$file:$function" in
    *internal/buildctx/buildctx.go:Prepare) baseline=94 ;;
    *cmd/apricot/up.go:runUp) baseline=376 ;;
    *cmd/apricot/up.go:buildRunArgs) baseline=147 ;;
    *internal/runner/runner_test.go:TestCommandArgv) baseline=88 ;;
  esac

  if (( baseline == 0 || lines > baseline )); then
    printf 'New or growing mikoto finding: %s\n' "$line" >&2
    failed=1
  else
    printf 'Existing length finding (max %d): %s\n' "$baseline" "$line"
  fi
done <<< "$output"

exit "$failed"
