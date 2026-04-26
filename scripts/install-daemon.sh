#!/usr/bin/env bash
# TapS Daemon installer — downloads latest release, sets up systemd service.
# Supports both fresh install and upgrade.
# Usage: curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install-daemon.sh | bash
set -euo pipefail

REPO="ProjectTapX/TapS"
INSTALL_DIR="/opt/taps"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

prompt() {
  local var="$1" msg="$2" default="$3" input=""
  if [[ -e /dev/tty ]]; then
    echo -n "$msg" >/dev/tty
    read -r input </dev/tty || true
  fi
  if [[ -z "$input" ]]; then input="$default"; fi
  eval "$var=\$input"
}

# --- Pre-checks ---
[[ $(id -u) -eq 0 ]] || error "Please run as root (sudo)"
command -v curl  >/dev/null || error "curl is required. Install it first."

# --- Install iptables if missing (required by Docker) ---
if ! command -v iptables >/dev/null 2>&1; then
  info "Installing iptables (required by Docker)..."
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update -qq && apt-get install -y -qq iptables >/dev/null
  elif command -v yum >/dev/null 2>&1; then
    yum install -y -q iptables >/dev/null
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y -q iptables >/dev/null
  else
    warn "Could not auto-install iptables. Please install it manually."
  fi
fi

# --- Install Docker if missing ---
if ! command -v docker >/dev/null 2>&1; then
  warn "Docker not found. Daemon requires Docker for container instances."
  INSTALL_DOCKER=""
  if [[ -e /dev/tty ]]; then
    echo -n "Install Docker now? [Y/n]: " >/dev/tty
    read -r INSTALL_DOCKER </dev/tty || true
  fi
  INSTALL_DOCKER="${INSTALL_DOCKER:-Y}"
  if [[ "$INSTALL_DOCKER" =~ ^[Yy]$ ]]; then
    info "Installing Docker via get.docker.com..."
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker
    info "Docker installed successfully."
  else
    warn "Skipping Docker installation. Daemon may not work properly without Docker."
  fi
fi

# --- Detect upgrade vs fresh install ---
UPGRADE=false
if [[ -x "${INSTALL_DIR}/daemon" ]] && systemctl is-active taps-daemon &>/dev/null; then
  UPGRADE=true
  info "Existing TapS Daemon detected. Running upgrade."
fi

# --- Detect architecture ---
case "$(uname -m)" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *) error "Unsupported architecture: $(uname -m). Only x86_64 and aarch64 are supported." ;;
esac
info "Detected architecture: ${ARCH}"

# --- Fetch latest version ---
info "Fetching latest release from GitHub..."
TAG=$(curl -fsSL "$API_URL" | grep '"tag_name"' | head -1 | cut -d'"' -f4)
[[ -n "$TAG" ]] || error "Failed to fetch latest release tag"
info "Latest version: ${TAG}"

if [[ "$UPGRADE" == true ]]; then
  # --- Upgrade mode ---
  info "Stopping taps-daemon..."
  systemctl stop taps-daemon

  DOWNLOAD_BASE="https://github.com/${REPO}/releases/download/${TAG}"
  info "Downloading daemon-linux-${ARCH}..."
  curl -fSL "${DOWNLOAD_BASE}/daemon-linux-${ARCH}" -o "${INSTALL_DIR}/daemon"
  chmod +x "${INSTALL_DIR}/daemon"

  info "Starting taps-daemon..."
  systemctl start taps-daemon
  sleep 3

  echo ""
  echo -e "${GREEN}============================================${NC}"
  echo -e "${GREEN} TapS Daemon upgraded to ${TAG}!${NC}"
  echo -e "${GREEN}============================================${NC}"
  echo -e "  Status: $(systemctl is-active taps-daemon 2>/dev/null || echo 'unknown')"
  echo ""
else
  # --- Fresh install mode ---
  echo ""
  echo -e "${CYAN}=== TapS Daemon Configuration ===${NC}"
  prompt DAEMON_ADDR "Daemon listen address [default: :24445]: " ":24445"
  prompt DAEMON_DATA "Daemon data directory [default: /var/lib/taps/daemon]: " "/var/lib/taps/daemon"
  echo ""

  # --- Download ---
  DOWNLOAD_BASE="https://github.com/${REPO}/releases/download/${TAG}"

  info "Downloading daemon-linux-${ARCH}..."
  mkdir -p "${INSTALL_DIR}"
  curl -fSL "${DOWNLOAD_BASE}/daemon-linux-${ARCH}" -o "${INSTALL_DIR}/daemon"
  chmod +x "${INSTALL_DIR}/daemon"

  # --- Create data directory ---
  mkdir -p "${DAEMON_DATA}"
  chmod 700 "${DAEMON_DATA}"

  # --- systemd unit ---
  info "Creating systemd service..."
  cat >/etc/systemd/system/taps-daemon.service <<UNIT
[Unit]
Description=TapS Daemon
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/daemon
WorkingDirectory=${INSTALL_DIR}
Environment=TAPS_DAEMON_DATA=${DAEMON_DATA}
Environment=TAPS_DAEMON_ADDR=${DAEMON_ADDR}
Environment=TAPS_REQUIRE_DOCKER=true
Restart=on-failure
RestartSec=3
TimeoutStopSec=30s
KillSignal=SIGTERM
User=root

[Install]
WantedBy=multi-user.target
UNIT

  systemctl daemon-reload
  systemctl enable --now taps-daemon
  sleep 3

  # --- Summary ---
  echo ""
  echo -e "${GREEN}============================================${NC}"
  echo -e "${GREEN} TapS Daemon installed successfully!${NC}"
  echo -e "${GREEN}============================================${NC}"
  echo ""
  echo -e "  Version:    ${CYAN}${TAG}${NC}"
  echo -e "  Arch:       ${CYAN}${ARCH}${NC}"
  echo -e "  Binary:     ${CYAN}${INSTALL_DIR}/daemon${NC}"
  echo -e "  Data dir:   ${CYAN}${DAEMON_DATA}${NC}"
  echo -e "  Listen:     ${CYAN}${DAEMON_ADDR}${NC}"
  echo ""

  TOKEN_FILE="${DAEMON_DATA}/token"
  if [[ -f "$TOKEN_FILE" ]]; then
    echo -e "  Token:      ${YELLOW}$(cat "$TOKEN_FILE")${NC}"
  else
    warn "Token file not found yet. Check: cat ${TOKEN_FILE}"
  fi

  FINGERPRINT=$(journalctl -u taps-daemon -n 30 --no-pager 2>/dev/null | grep -i "tls fingerprint" | tail -1 | grep -oP '(?:fingerprint:?\s*)(\S+)' | tail -1 || true)
  if [[ -n "$FINGERPRINT" ]]; then
    echo -e "  TLS FP:     ${YELLOW}${FINGERPRINT}${NC}"
  else
    warn "TLS fingerprint not found in logs. Check: journalctl -u taps-daemon | grep fingerprint"
  fi

  echo ""
  echo -e "  Status:     $(systemctl is-active taps-daemon 2>/dev/null || echo 'unknown')"
  echo ""
  info "Add this daemon to your Panel via Node Management."
fi
