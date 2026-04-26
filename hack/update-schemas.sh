#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

OLDGOFLAGS="${GOFLAGS:-}"
GOFLAGS=""

cd "$(git rev-parse --show-toplevel)"

CONTROLLER_GEN_VERSION="v0.17.1"
TEMP_DIR_LOGS=$(mktemp -d)

echo "=== Generating deepcopy ==="
go run sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_GEN_VERSION} \
  object \
  paths=./pkg/apis/agent/v1alpha1/

echo "=== Generating CRD schemas with OpenAPI validation ==="

CRD_FILES=(
  "config/300-repository.yaml"
  "config/300-agent.yaml"
  "config/300-agentrun.yaml"
)

for FILENAME in "${CRD_FILES[@]}"; do
  BASENAME=$(basename "$FILENAME")
  echo "Generating OpenAPI schema for $BASENAME"

  GROUP=$(grep -E '^  group:' "$FILENAME")
  GROUP=${GROUP#"  group: "}
  API_SUBDIR=${GROUP%".tekton.dev"}

  PLURAL=$(yq eval '.spec.names.plural' "$FILENAME")

  TEMP_DIR=$(mktemp -d)
  cp -p "$FILENAME" "$TEMP_DIR/."
  LOG_FILE="$TEMP_DIR_LOGS/log-schema-generation-$BASENAME"

  echo "  Processing API group: $GROUP, subdir: $API_SUBDIR, plural: $PLURAL"

  counter=0 limit=5
  while [ "$counter" -lt "$limit" ]; do
    set +e
    go run sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_GEN_VERSION} \
      crd:crdVersions=v1 \
      output:crd:artifacts:config="$TEMP_DIR" \
      paths="./pkg/apis/$API_SUBDIR/..." >"$LOG_FILE" 2>&1
    rc=$?
    set -e

    if [ $rc -eq 0 ]; then
      echo "  Successfully generated schema"

      if command -v yq >/dev/null 2>&1 && yq --version | grep -q "mikefarah/yq"; then
        AUTO_GENERATED_CRD="$TEMP_DIR/${GROUP}_${PLURAL}.yaml"

        if [ -n "$AUTO_GENERATED_CRD" ] && [ -f "$AUTO_GENERATED_CRD" ]; then
          yq eval '.spec.versions[0].schema' "$AUTO_GENERATED_CRD" >/tmp/schema.yaml
          yq eval -i '.spec.versions[0].schema = load("/tmp/schema.yaml")' "$FILENAME"
          rm -f /tmp/schema.yaml
          echo "  Schema synced to $BASENAME"
        else
          echo "  Warning: Auto-generated CRD not found in temporary directory"
        fi
      else
        echo "  Warning: mikefarah/yq not available, cannot automatically sync schema"
      fi

      # Clean up any auto-generated CRD files from the config directory
      go run sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_GEN_VERSION} \
        crd:crdVersions=v1 \
        output:crd:artifacts:config="config" \
        paths="./pkg/apis/$API_SUBDIR/..." >/dev/null 2>&1

      for AUTO_CRD in $(find "config" -name "${GROUP}_*.yaml"); do
        echo "  Removing auto-generated CRD file: $(basename "$AUTO_CRD")"
        rm -f "$AUTO_CRD"
      done

      break
    fi

    if grep -q 'exit status 1' "$LOG_FILE"; then
      echo "  Warning: Encountered errors during schema generation"
      echo "  Check $LOG_FILE for details"
      break
    fi

    counter=$((counter + 1))
    if [ $counter -eq $limit ]; then
      echo "  Failed to generate CRD schema after $limit attempts"
      cat "$LOG_FILE"
      exit 1
    fi

    echo "  Retrying (attempt $counter of $limit)..."
    sleep 1
  done

  rm -rf "$TEMP_DIR"
done

echo "=== Done ==="
echo "Log files available at: $TEMP_DIR_LOGS"

GOFLAGS="${OLDGOFLAGS}"
