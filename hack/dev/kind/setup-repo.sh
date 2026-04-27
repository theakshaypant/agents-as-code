#!/usr/bin/env bash
# Creates a Repository CR and its associated secrets from .env values.
#
# Required .env variables:
#   REPO_URL              — HTTPS URL of the GitHub repository
#   GITHUB_TOKEN          — PAT with repo scope
#   GITHUB_WEBHOOK_SECRET — webhook secret configured on GitHub
#
# Optional:
#   REPO_NAME      — CR name (default: derived from REPO_URL)
#   REPO_NAMESPACE — namespace for the CR (default: agents-as-code-system)
#   AI_ENABLED     — enable AI agents (default: false)
#   AI_PROVIDER    — LLM provider name (e.g. anthropic, openai)
#   AI_API_KEY     — LLM API key
set -euf
cd $(dirname $(readlink -f ${0}))

AAC_DIR=$(cd ../../.. && pwd)
ENV_FILE="${AAC_DIR}/.env"

if [[ -f "${ENV_FILE}" ]]; then
  set -a
  source "${ENV_FILE}"
  set +a
fi

export KUBECONFIG=${KUBECONFIG:-${HOME}/.kube/config}

if [[ -z "${REPO_URL:-}" ]]; then
  echo "Error: REPO_URL is not set in .env"
  exit 1
fi
if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  echo "Error: GITHUB_TOKEN is not set in .env"
  exit 1
fi
if [[ -z "${GITHUB_WEBHOOK_SECRET:-}" ]]; then
  echo "Error: GITHUB_WEBHOOK_SECRET is not set in .env"
  exit 1
fi

NAMESPACE=${REPO_NAMESPACE:-agents-as-code-system}

if [[ -n "${REPO_NAME:-}" ]]; then
  NAME="${REPO_NAME}"
else
  NAME=$(echo "${REPO_URL}" | sed 's|.*/||; s|\.git$||')
fi

TOKEN_SECRET_NAME="${NAME}-git-token"
WEBHOOK_SECRET_NAME="${NAME}-webhook-secret"
AI_SECRET_NAME="${NAME}-ai-key"

echo "Setting up Repository CR: ${NAME} in ${NAMESPACE}"
echo "  URL: ${REPO_URL}"

kubectl create secret generic "${TOKEN_SECRET_NAME}" \
  -n "${NAMESPACE}" \
  --from-literal=token="${GITHUB_TOKEN}" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl create secret generic "${WEBHOOK_SECRET_NAME}" \
  -n "${NAMESPACE}" \
  --from-literal=webhook.secret="${GITHUB_WEBHOOK_SECRET}" \
  --dry-run=client -o yaml | kubectl apply -f -

AI_SETTINGS=""
if [[ "${AI_ENABLED:-false}" == "true" ]]; then
  if [[ -z "${AI_API_KEY:-}" ]]; then
    echo "Error: AI_ENABLED=true but AI_API_KEY is not set in .env"
    exit 1
  fi

  kubectl create secret generic "${AI_SECRET_NAME}" \
    -n "${NAMESPACE}" \
    --from-literal=api-key="${AI_API_KEY}" \
    --dry-run=client -o yaml | kubectl apply -f -

  AI_SETTINGS=$(cat <<AIEOF
  settings:
    ai:
      enabled: true
      provider: "${AI_PROVIDER:-gemini}"
      secret_ref:
        name: "${AI_SECRET_NAME}"
        key: "api-key"
AIEOF
  )
fi

cat <<EOF | kubectl apply -f -
apiVersion: agent.tekton.dev/v1alpha1
kind: Repository
metadata:
  name: ${NAME}
  namespace: ${NAMESPACE}
spec:
  url: "${REPO_URL}"
  git_provider:
    secret:
      name: "${TOKEN_SECRET_NAME}"
      key: "token"
    webhook_secret:
      name: "${WEBHOOK_SECRET_NAME}"
      key: "webhook.secret"
${AI_SETTINGS}
EOF

echo ""
echo "Created:"
echo "  Secret:     ${NAMESPACE}/${TOKEN_SECRET_NAME}"
echo "  Secret:     ${NAMESPACE}/${WEBHOOK_SECRET_NAME}"
if [[ "${AI_ENABLED:-false}" == "true" ]]; then
  echo "  Secret:     ${NAMESPACE}/${AI_SECRET_NAME}"
fi
echo "  Repository: ${NAMESPACE}/${NAME}"
