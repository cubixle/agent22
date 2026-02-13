#!/usr/bin/env bash

set -euo pipefail

BIN_DIR="${HOME}/.local/bin"
BIN_NAME="agent22"
SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p "${BIN_DIR}"

echo "Building ${BIN_NAME}..."
go build -o "${BIN_DIR}/${BIN_NAME}" "${SRC_DIR}"

chmod +x "${BIN_DIR}/${BIN_NAME}"

echo "Installed ${BIN_NAME} to ${BIN_DIR}/${BIN_NAME}"
echo "If needed, add ${BIN_DIR} to your PATH."
