#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

OLDGOFLAGS="${GOFLAGS:-}"
GOFLAGS=""

SCRIPT_DIR=$(dirname "${BASH_SOURCE[0]}")
CRD_PATH="${SCRIPT_DIR}/../config"
API_PATH="${SCRIPT_DIR}/../pkg/apis"
CONTROLLER_GEN_VERSION="v0.17.1"

echo "=== Generating deepcopy ==="
go run sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_GEN_VERSION} \
  object \
  paths=${API_PATH}/agent/v1alpha1/

echo "=== Generating CRD schemas ==="

CRD_FILES=(
  "${CRD_PATH}/300-repository.yaml"
)

for FILENAME in "${CRD_FILES[@]}"; do
  BASENAME=$(basename "$FILENAME")
  echo "Generating OpenAPI schema for $BASENAME"

  GROUP=$(grep -E '^  group:' "$FILENAME")
  GROUP=${GROUP#"  group: "}
  API_SUBDIR=${GROUP%".tekton.dev"}

  TEMP_DIR=$(mktemp -d)

  go run sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_GEN_VERSION} \
    crd:crdVersions=v1 \
    output:crd:artifacts:config="$TEMP_DIR" \
    paths="${API_PATH}/${API_SUBDIR}/..."

  if command -v yq >/dev/null 2>&1 && yq --version | grep -q "mikefarah/yq"; then
    AUTO_GENERATED_CRD=$(find "$TEMP_DIR" -name "${GROUP}_*.yaml")
    if [ -f "$AUTO_GENERATED_CRD" ]; then
      yq eval '.spec.versions[0].schema' "$AUTO_GENERATED_CRD" >/tmp/schema.yaml
      yq eval -i '.spec.versions[0].schema = load("/tmp/schema.yaml")' "$FILENAME"
      rm -f /tmp/schema.yaml
      echo "  Schema synced to $BASENAME"
    else
      echo "  Warning: Auto-generated CRD not found"
    fi
  else
    echo "  Warning: mikefarah/yq not available, manually copy schema from $TEMP_DIR"
  fi

  rm -rf "$TEMP_DIR"
done

echo "=== Done ==="
GOFLAGS="${OLDGOFLAGS}"
