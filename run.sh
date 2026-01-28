#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

IN_XLSX="${1:-}" 
FLAG="${2:-}"

if [[ -z "${IN_XLSX}" ]]; then
  IN_XLSX="${SCRIPT_DIR}/xls"
fi

# Allow passing only filename: default to ./xls/<name>
if [[ ! -f "${IN_XLSX}" && ! -d "${IN_XLSX}" ]]; then
  CANDIDATE="${SCRIPT_DIR}/xls/$(basename "${IN_XLSX}")"
  if [[ -f "${CANDIDATE}" || -d "${CANDIDATE}" ]]; then
    IN_XLSX="${CANDIDATE}"
  else
    echo "Input path not found: ${IN_XLSX} (also tried ${CANDIDATE})" >&2
    echo "Usage: $(basename "$0") [file.xlsx|dir] [flag(server|client)]" >&2
    echo "Default: $(basename "$0") (will use ./xls)" >&2
    exit 2
  fi
fi

OUT_DIR="${SCRIPT_DIR}/out"
mkdir -p "${OUT_DIR}"

ARGS=(--in "${IN_XLSX}" --out "${OUT_DIR}" --lang all --pkg config --json=true -v)

if [[ -n "${FLAG}" ]]; then
  ARGS+=(--flag "${FLAG}")
fi

echo "Running: go run . ${ARGS[*]}" >&2
( cd "${SCRIPT_DIR}" && go run . "${ARGS[@]}" )

echo "Done. Outputs:" >&2
ls -la "${OUT_DIR}" >&2
