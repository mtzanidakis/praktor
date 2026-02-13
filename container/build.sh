#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_NAME="${1:-praktor-agent:latest}"

echo "Building agent image: ${IMAGE_NAME}"
docker build -t "${IMAGE_NAME}" "${SCRIPT_DIR}"
echo "Done: ${IMAGE_NAME}"
