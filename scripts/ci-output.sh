#!/bin/sh

run_check() {
  label=$1
  shift

  printf '\n[check] %s\n' "$label"
  if "$@"; then
    printf '[pass] %s\n' "$label"
  else
    result=$?
    printf '[FAIL] %s (exit %s)\n' "$label" "$result" >&2
    return "$result"
  fi
}
