#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="oci-grabber"
REPO_URL="${REPO_URL:-https://github.com/Ioveo/jgw.git}"
BRANCH="${BRANCH:-main}"
SOURCE_DIR="${SOURCE_DIR:-/opt/${APP_NAME}-src}"
GO_VERSION="${GO_VERSION:-1.21.13}"
CONFIG_FILE="${CONFIG_FILE:-config.toml}"

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

fail() {
  printf '[ERROR] %s\n' "$*" >&2
  exit 1
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    fail "Please run as root, for example: curl -fsSL ... | sudo bash"
  fi
}

install_packages() {
  local packages=(ca-certificates curl git tar)

  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y "${packages[@]}"
    return
  fi

  if command -v dnf >/dev/null 2>&1; then
    dnf install -y "${packages[@]}"
    return
  fi

  if command -v yum >/dev/null 2>&1; then
    yum install -y "${packages[@]}"
    return
  fi

  fail "Unsupported Linux distribution. Please install curl, git and tar first."
}

go_arch() {
  case "$(uname -m)" in
    x86_64|amd64)
      printf 'amd64'
      ;;
    aarch64|arm64)
      printf 'arm64'
      ;;
    *)
      fail "Unsupported CPU architecture: $(uname -m)"
      ;;
  esac
}

go_version_ok() {
  command -v go >/dev/null 2>&1 || return 1

  local version major minor
  version="$(go version | awk '{print $3}' | sed 's/^go//')"
  major="${version%%.*}"
  minor="${version#*.}"
  minor="${minor%%.*}"

  (( major > 1 || (major == 1 && minor >= 21) ))
}

install_go_if_needed() {
  if go_version_ok; then
    log "Using existing Go: $(go version)"
    return
  fi

  local arch tarball url
  arch="$(go_arch)"
  tarball="/tmp/go${GO_VERSION}.linux-${arch}.tar.gz"
  url="https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz"

  log "Installing Go ${GO_VERSION} for linux-${arch}"
  curl -fsSL "${url}" -o "${tarball}"
  rm -rf /usr/local/go
  tar -C /usr/local -xzf "${tarball}"
  rm -f "${tarball}"
  export PATH="/usr/local/go/bin:${PATH}"

  go_version_ok || fail "Go installation failed"
}

sync_repo() {
  if [[ -d "${SOURCE_DIR}/.git" ]]; then
    log "Updating source code in ${SOURCE_DIR}"
    git -C "${SOURCE_DIR}" fetch --prune origin "${BRANCH}"
    git -C "${SOURCE_DIR}" checkout "${BRANCH}"
    git -C "${SOURCE_DIR}" reset --hard "origin/${BRANCH}"
  else
    log "Cloning ${REPO_URL} to ${SOURCE_DIR}"
    rm -rf "${SOURCE_DIR}"
    git clone --branch "${BRANCH}" --depth 1 "${REPO_URL}" "${SOURCE_DIR}"
  fi
}

ensure_config() {
  if [[ -f "${SOURCE_DIR}/${CONFIG_FILE}" ]]; then
    return
  fi

  if [[ -f "${SOURCE_DIR}/config.example.toml" ]]; then
    cp "${SOURCE_DIR}/config.example.toml" "${SOURCE_DIR}/${CONFIG_FILE}"
    chmod 600 "${SOURCE_DIR}/${CONFIG_FILE}"
    log "Created ${SOURCE_DIR}/${CONFIG_FILE} from config.example.toml"
    log "Please edit it before starting the service if your real OCI values are not filled yet."
  fi
}

main() {
  require_root
  install_packages
  install_go_if_needed
  sync_repo
  ensure_config

  log "Running deploy script"
  cd "${SOURCE_DIR}"
  bash scripts/deploy-linux.sh --config "${CONFIG_FILE}"
}

main "$@"
