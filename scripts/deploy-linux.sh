#!/usr/bin/env bash
set -Eeuo pipefail

APP_NAME="oci-grabber"
SERVICE_NAME="${APP_NAME}.service"
INSTALL_DIR="/opt/${APP_NAME}"
CONFIG_FILE="config.toml"
RUN_USER="oci-grabber"
GO_VERSION_MIN="1.21"
ACTION="install"

usage() {
  cat <<EOF
Usage:
  sudo bash scripts/deploy-linux.sh [options]

Options:
  --config PATH        Config file to install. Default: ./config.toml
  --install-dir PATH   Install directory. Default: /opt/oci-grabber
  --user USER          Linux user used by systemd. Default: oci-grabber
  --action ACTION      install | restart | stop | status | uninstall. Default: install
  -h, --help           Show this help.

Examples:
  sudo bash scripts/deploy-linux.sh
  sudo bash scripts/deploy-linux.sh --config /root/jgw/config.toml
  sudo bash scripts/deploy-linux.sh --action status
EOF
}

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"
}

fail() {
  printf '[ERROR] %s\n' "$*" >&2
  exit 1
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    fail "Please run this script as root, for example: sudo bash scripts/deploy-linux.sh"
  fi
}

repo_root() {
  local script_dir
  script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
  cd "${script_dir}/.." && pwd
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --config)
        CONFIG_FILE="${2:?Missing value for --config}"
        shift 2
        ;;
      --install-dir)
        INSTALL_DIR="${2:?Missing value for --install-dir}"
        shift 2
        ;;
      --user)
        RUN_USER="${2:?Missing value for --user}"
        shift 2
        ;;
      --action)
        ACTION="${2:?Missing value for --action}"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        fail "Unknown option: $1"
        ;;
    esac
  done
}

ensure_command() {
  command -v "$1" >/dev/null 2>&1 || fail "Missing required command: $1"
}

ensure_go() {
  ensure_command go

  local version
  version="$(go version | awk '{print $3}' | sed 's/^go//')"
  local min_major min_minor major minor
  min_major="${GO_VERSION_MIN%%.*}"
  min_minor="${GO_VERSION_MIN#*.}"
  major="${version%%.*}"
  minor="${version#*.}"
  minor="${minor%%.*}"

  if (( major < min_major || (major == min_major && minor < min_minor) )); then
    fail "Go ${GO_VERSION_MIN}+ is required, current version is ${version}"
  fi
}

ensure_user() {
  if ! id -u "${RUN_USER}" >/dev/null 2>&1; then
    log "Creating system user: ${RUN_USER}"
    useradd --system --home-dir "${INSTALL_DIR}" --shell /usr/sbin/nologin "${RUN_USER}"
  fi
}

install_files() {
  local root="$1"
  local source_config="$2"
  local binary_path="${root}/${APP_NAME}"

  [[ -f "${source_config}" ]] || fail "Config file not found: ${source_config}"

  log "Building ${APP_NAME}"
  cd "${root}"
  go build -trimpath -ldflags="-s -w" -o "${binary_path}" .

  log "Installing files to ${INSTALL_DIR}"
  install -d -m 0750 -o "${RUN_USER}" -g "${RUN_USER}" "${INSTALL_DIR}"
  install -m 0755 "${binary_path}" "${INSTALL_DIR}/${APP_NAME}"
  install -m 0640 -o "${RUN_USER}" -g "${RUN_USER}" "${source_config}" "${INSTALL_DIR}/config.toml"

  copy_private_key_if_local "${source_config}"
  chown -R "${RUN_USER}:${RUN_USER}" "${INSTALL_DIR}"
}

copy_private_key_if_local() {
  local source_config="$1"
  local key_path
  key_path="$(awk -F '=' '
    $1 ~ /^[[:space:]]*private_key_path[[:space:]]*$/ {
      gsub(/[[:space:]"]/, "", $2)
      print $2
      exit
    }
  ' "${source_config}")"

  if [[ -z "${key_path}" ]]; then
    return
  fi

  if [[ "${key_path}" = /* ]]; then
    if [[ -f "${key_path}" ]]; then
      log "Keeping absolute private key path from config: ${key_path}"
    else
      log "Private key path is absolute but not found on this machine: ${key_path}"
    fi
    return
  fi

  local config_dir
  config_dir="$(cd -- "$(dirname -- "${source_config}")" && pwd)"
  local source_key="${config_dir}/${key_path}"

  if [[ ! -f "${source_key}" ]]; then
    log "Private key file not found beside config, skip copy: ${source_key}"
    return
  fi

  log "Copying private key to ${INSTALL_DIR}/${key_path}"
  install -m 0600 -o "${RUN_USER}" -g "${RUN_USER}" "${source_key}" "${INSTALL_DIR}/${key_path}"
}

write_service() {
  local service_path="/etc/systemd/system/${SERVICE_NAME}"

  log "Writing systemd service: ${service_path}"
  cat > "${service_path}" <<EOF
[Unit]
Description=OCI Instance Auto-Grabber
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=${RUN_USER}
Group=${RUN_USER}
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/${APP_NAME} -config ${INSTALL_DIR}/config.toml
Restart=always
RestartSec=10
KillSignal=SIGTERM
TimeoutStopSec=30
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=${INSTALL_DIR}

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
}

install_service() {
  require_root
  ensure_command systemctl
  ensure_go
  ensure_user

  local root
  root="$(repo_root)"
  local source_config="${CONFIG_FILE}"
  if [[ "${source_config}" != /* ]]; then
    source_config="${root}/${source_config}"
  fi

  install_files "${root}" "${source_config}"
  write_service

  log "Enabling and starting ${SERVICE_NAME}"
  systemctl enable --now "${SERVICE_NAME}"
  systemctl --no-pager --full status "${SERVICE_NAME}" || true

  log "Done. View logs with: journalctl -u ${SERVICE_NAME} -f"
}

restart_service() {
  require_root
  systemctl restart "${SERVICE_NAME}"
  systemctl --no-pager --full status "${SERVICE_NAME}" || true
}

stop_service() {
  require_root
  systemctl stop "${SERVICE_NAME}"
}

status_service() {
  systemctl --no-pager --full status "${SERVICE_NAME}" || true
}

uninstall_service() {
  require_root
  systemctl disable --now "${SERVICE_NAME}" 2>/dev/null || true
  rm -f "/etc/systemd/system/${SERVICE_NAME}"
  systemctl daemon-reload
  log "Service removed. Installed files are kept at ${INSTALL_DIR}"
}

main() {
  parse_args "$@"

  case "${ACTION}" in
    install)
      install_service
      ;;
    restart)
      restart_service
      ;;
    stop)
      stop_service
      ;;
    status)
      status_service
      ;;
    uninstall)
      uninstall_service
      ;;
    *)
      fail "Unknown action: ${ACTION}"
      ;;
  esac
}

main "$@"
