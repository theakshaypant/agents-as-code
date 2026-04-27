#!/usr/bin/env bash
# Sets up a kind cluster with agents-as-code deployed via ko.
#
# Prerequisites:
#   - kind: https://kind.sigs.k8s.io/docs/user/quick-start/#installation
#   - ko: https://ko.build/install/
#   - gosmee: https://github.com/chmouel/gosmee
#
# Webhook forwarding:
#   1. Create a smee URL at https://hook.pipelinesascode.com
#   2. Export it: export AAC_SMEEURL=https://hook.pipelinesascode.com/aBcDeF
#   3. Run this script
#   4. In a separate terminal:
#      gosmee client --saveDir /tmp/replays $AAC_SMEEURL http://webhook.aac-127-0-0-1.nip.io
#
# Secrets:
#   Copy .env.example to .env at the repo root and fill in:
#     GITHUB_APP_ID, GITHUB_PRIVATE_KEY_PATH, GITHUB_WEBHOOK_SECRET
#   The script reads .env automatically. Keep .env out of git (.gitignore).
set -euf
cd $(dirname $(readlink -f ${0}))

AAC_DIR=$(cd ../../.. && pwd)
ENV_FILE="${AAC_DIR}/.env"

if [[ -f "${ENV_FILE}" ]]; then
  set -a
  source "${ENV_FILE}"
  set +a
fi

export KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-aac}
export KUBECONFIG=${KUBECONFIG:-${HOME}/.kube/config}
export DOMAIN_NAME=aac-127-0-0-1.nip.io

if [[ -z "${AAC_SMEEURL:-}" ]]; then
  echo "You need to forward webhooks via smee."
  echo "Create a URL at https://hook.pipelinesascode.com"
  echo "Then export it: AAC_SMEEURL=https://hook.pipelinesascode.com/XXXXXXXX"
  echo "Alternatively: AAC_SMEEURL=\$(curl https://hook.pipelinesascode.com -o=/dev/null -sw '%{redirect_url}')"
  exit 1
fi

for tool in kind ko gosmee; do
  if ! builtin type -p ${tool} &>/dev/null; then
    echo "Install ${tool} before running this script."
    exit 1
  fi
done

kind=$(type -p kind)
ko=$(type -p ko)

TMPD=$(mktemp -d /tmp/.AACXXXX)
REG_PORT=${REG_PORT:-'5000'}
REG_NAME='kind-registry'
NO_REINSTALL_KIND=${NO_REINSTALL_KIND:-""}
SUDO=sudo

[[ $(uname -s) == "Darwin" ]] && {
  SUDO=
}

cleanup() { rm -rf ${TMPD}; }
trap cleanup EXIT

function start_registry() {
  running="$(docker inspect -f '{{.State.Running}}' ${REG_NAME} 2>/dev/null || echo false)"

  if [[ ${running} != "true" ]]; then
    docker rm -f kind-registry || true
    docker run \
      -d --restart=always -p "127.0.0.1:${REG_PORT}:5000" \
      -e REGISTRY_HTTP_SECRET=secret \
      --name "${REG_NAME}" \
      registry:2
  fi
}

function reinstall_kind() {
  ${SUDO} ${kind} delete cluster --name ${KIND_CLUSTER_NAME} || true
  sed "s,%DOCKERCFG%,${HOME}/.docker/config.json," kind.yaml >${TMPD}/kconfig.yaml

  cat <<EOF >>${TMPD}/kconfig.yaml
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:${REG_PORT}"]
    endpoint = ["http://${REG_NAME}:5000"]
EOF

  ${SUDO} ${kind} create cluster --name ${KIND_CLUSTER_NAME} --config ${TMPD}/kconfig.yaml

  # Merge kind context into default kubeconfig and set as current
  ${SUDO} ${kind} --name ${KIND_CLUSTER_NAME} get kubeconfig >${TMPD}/kind-kubeconfig
  mkdir -p $(dirname ${KUBECONFIG})
  if [[ -f "${KUBECONFIG}" ]]; then
    KUBECONFIG="${TMPD}/kind-kubeconfig:${KUBECONFIG}" kubectl config view --flatten >${TMPD}/merged-kubeconfig
    cp "${TMPD}/merged-kubeconfig" "${KUBECONFIG}"
  else
    cp "${TMPD}/kind-kubeconfig" "${KUBECONFIG}"
  fi
  kubectl config use-context "kind-${KIND_CLUSTER_NAME}"

  docker network connect "kind" "${REG_NAME}" 2>/dev/null || true
  cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${REG_PORT}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF
}

function install_nginx() {
  echo "Installing nginx ingress"
  kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml >/dev/null
  i=0
  echo -n "Waiting for nginx to come up: "
  while true; do
    [[ ${i} == 120 ]] && exit 1
    ep=$(kubectl wait --namespace ingress-nginx --for=condition=ready pod --selector=app.kubernetes.io/component=controller --timeout=180s 2>/dev/null || true)
    [[ -n ${ep} ]] && break
    sleep 5
    i=$((i + 1))
  done
  echo "done."
}

function install_aac() {
  echo "Deploying agents-as-code from ${AAC_DIR}"
  oldPwd=${PWD}
  cd ${AAC_DIR}
  env KO_DOCKER_REPO=localhost:${REG_PORT} ${ko} apply -f config --sbom=none -B >/dev/null
  cd ${oldPwd}
  configure_aac
  echo "webhook: http://webhook.${DOMAIN_NAME}"
}

function configure_aac() {
  sed -e "s,%DOMAIN_NAME%,${DOMAIN_NAME}," ingress-webhook.yaml | kubectl apply -f-

  if [[ -n "${GITHUB_WEBHOOK_SECRET:-}" ]]; then
    echo "Installing agents-as-code-github-app secret from .env"
    kubectl delete secret agents-as-code-github-app -n agents-as-code-system 2>/dev/null || true

    secret_args=(
      --from-literal=webhook.secret="${GITHUB_WEBHOOK_SECRET}"
    )
    if [[ -n "${GITHUB_APP_ID:-}" ]]; then
      secret_args+=(--from-literal=github-application-id="${GITHUB_APP_ID}")
    fi
    if [[ -n "${GITHUB_PRIVATE_KEY_PATH:-}" ]]; then
      if [[ ! -f "${GITHUB_PRIVATE_KEY_PATH}" ]]; then
        echo "Error: GITHUB_PRIVATE_KEY_PATH=${GITHUB_PRIVATE_KEY_PATH} does not exist"
        exit 1
      fi
      secret_args+=(--from-file=github-private-key="${GITHUB_PRIVATE_KEY_PATH}")
    fi

    kubectl create secret generic agents-as-code-github-app -n agents-as-code-system "${secret_args[@]}"
  else
    echo "No secret installed (GITHUB_WEBHOOK_SECRET not set in .env)."
    echo "Copy .env.example to .env and fill in the values, or create the secret manually:"
    echo "  kubectl create secret generic agents-as-code-github-app -n agents-as-code-system \\"
    echo "    --from-literal=webhook.secret=<secret>"
  fi

  echo "Set active namespace to agents-as-code-system"
  kubectl config set-context --current --namespace=agents-as-code-system >/dev/null
  echo "Run: gosmee client --saveDir /tmp/replays ${AAC_SMEEURL} http://webhook.${DOMAIN_NAME}"
}

main() {
  if [[ -z ${NO_REINSTALL_KIND} ]]; then
    start_registry
    reinstall_kind
  else
    echo "Skipping kind reinstall"
  fi
  install_nginx
  install_aac
  echo ""
  echo "Done!"
  echo "Using registry on localhost:${REG_PORT}"
}

function usage() {
  cat <<EOF
Usage: $0 [OPTIONS]

Options:
  -h          Show this message
  -b          Only install registry/kind/nginx (no agents-as-code)
  -c          Configure agents-as-code only (ingress + secrets)
  -y          Install only agents-as-code (ko apply + configure)
  -R          Restart agents-as-code pods
  -O          Don't reinstall kind, continue with nginx + agents-as-code
EOF
}

while getopts "hbcyRO" o; do
  case "${o}" in
  h)
    usage
    exit
    ;;
  b)
    start_registry
    reinstall_kind
    install_nginx
    exit
    ;;
  c)
    configure_aac
    exit
    ;;
  y)
    install_aac
    exit
    ;;
  R)
    echo "Restarting agents-as-code pods"
    kubectl delete pod -l app.kubernetes.io/part-of=agents-as-code -n agents-as-code-system || true
    exit
    ;;
  O)
    NO_REINSTALL_KIND=yes
    ;;
  *)
    echo "Invalid option"
    exit 1
    ;;
  esac
done
shift $((OPTIND - 1))

main
