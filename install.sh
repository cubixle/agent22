#!/usr/bin/env bash

set -euo pipefail

BIN_DIR="${HOME}/.local/bin"
BIN_NAME="${1:-agent22}"
SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$(mktemp -d)"
BUILD_PATH="${BUILD_DIR}/${BIN_NAME}"
TARGET_PATH="${BIN_DIR}/${BIN_NAME}"

trap 'rm -rf "${BUILD_DIR}"' EXIT

mkdir -p "${BIN_DIR}"

echo "Building ${BIN_NAME}..."
go build -o "${BUILD_PATH}" "${SRC_DIR}"

echo "Installing ${BIN_NAME}..."
mv "${BUILD_PATH}" "${TARGET_PATH}"

chmod +x "${TARGET_PATH}"

echo "Installed ${BIN_NAME} to ${TARGET_PATH}"
echo "If needed, add ${BIN_DIR} to your PATH."
