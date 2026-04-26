#!/usr/bin/env bash
# TapS single-host installer — Panel + Daemon on the same machine.
# Usage: curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install.sh | bash
set -euo pipefail

REPO="ProjectTapX/TapS"
INSTALL_DIR="/opt/taps"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

# --- Pre-checks ---
[[ $(id -u) -eq 0 ]] || error "Please run as root (sudo)"
command -v curl  >/dev/null || error "curl is required. Install it first."

# --- Install Docker if missing ---
if ! command -v docker >/dev/null 2>&1; then
  warn "Docker not found. Daemon requires Docker for container instances."
  read -rp "Install Docker now? [Y/n]: " INSTALL_DOCKER </dev/tty 2>/dev/null || true
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

# --- Interactive configuration ---
# Read from /dev/tty so it works when piped via curl | bash
prompt() {
  local var="$1" msg="$2" default="$3"
  if [[ -t 0 ]] || [[ -e /dev/tty ]]; then
    read -rp "$msg" "$var" </dev/tty 2>/dev/null || true
  fi
  eval "[[ -z \"\$$var\" ]] && $var='$default'"
}
prompt_secret() {
  local var="$1" msg="$2" default="$3"
  if [[ -t 0 ]] || [[ -e /dev/tty ]]; then
    read -rsp "$msg" "$var" </dev/tty 2>/dev/null || true
    echo "" >/dev/tty 2>/dev/null || true
  fi
  eval "[[ -z \"\$$var\" ]] && $var='$default'"
}

echo ""
echo -e "${CYAN}=== TapS Single-Host Configuration ===${NC}"
echo ""
echo -e "${CYAN}-- Panel --${NC}"
prompt PANEL_PORT "Panel listen port [default: 24444]: " "24444"
prompt PANEL_DATA "Panel data directory [default: /var/lib/taps/panel]: " "/var/lib/taps/panel"
prompt WEB_DIR    "Web static directory [default: /opt/taps/web]: " "/opt/taps/web"
prompt ADMIN_USER "Admin username [default: admin]: " "admin"
prompt_secret ADMIN_PASS "Admin password [default: admin]: " "admin"
echo ""
echo -e "${CYAN}-- Daemon --${NC}"
prompt DAEMON_ADDR "Daemon listen address [default: :24445]: " ":24445"
prompt DAEMON_DATA "Daemon data directory [default: /var/lib/taps/daemon]: " "/var/lib/taps/daemon"
echo ""

# --- Download ---
DOWNLOAD_BASE="https://github.com/${REPO}/releases/download/${TAG}"

info "Downloading panel-linux-${ARCH}..."
mkdir -p "${INSTALL_DIR}"
curl -fSL "${DOWNLOAD_BASE}/panel-linux-${ARCH}" -o "${INSTALL_DIR}/panel"
chmod +x "${INSTALL_DIR}/panel"

info "Downloading daemon-linux-${ARCH}..."
curl -fSL "${DOWNLOAD_BASE}/daemon-linux-${ARCH}" -o "${INSTALL_DIR}/daemon"
chmod +x "${INSTALL_DIR}/daemon"

info "Downloading web.tar.gz..."
curl -fSL "${DOWNLOAD_BASE}/web.tar.gz" -o /tmp/taps-web.tar.gz
rm -rf "${WEB_DIR}"
mkdir -p "${WEB_DIR}"
tar -xzf /tmp/taps-web.tar.gz -C "${WEB_DIR}"
rm -f /tmp/taps-web.tar.gz

# --- Create data directories ---
mkdir -p "${PANEL_DATA}" "${DAEMON_DATA}"
chmod 700 "${PANEL_DATA}" "${DAEMON_DATA}"

# --- systemd units ---
info "Creating systemd services..."

cat >/etc/systemd/system/taps-daemon.service <<EOF
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
EOF

cat >/etc/systemd/system/taps-panel.service <<EOF
[Unit]
Description=TapS Panel
After=network-online.target taps-daemon.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/panel
WorkingDirectory=${INSTALL_DIR}
Environment=TAPS_DATA_DIR=${PANEL_DATA}
Environment=TAPS_WEB_DIR=${WEB_DIR}
Environment=TAPS_ADDR=:${PANEL_PORT}
Environment=TAPS_ADMIN_USER=${ADMIN_USER}
Environment=TAPS_ADMIN_PASS=${ADMIN_PASS}
Restart=on-failure
RestartSec=3
TimeoutStopSec=30s
KillSignal=SIGTERM
User=root

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload

# --- Start (daemon first) ---
info "Starting TapS Daemon..."
systemctl enable --now taps-daemon
sleep 3

info "Starting TapS Panel..."
systemctl enable --now taps-panel
sleep 3

# --- Summary ---
HOSTNAME=$(hostname -I 2>/dev/null | awk '{print $1}' || hostname)

echo ""
echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN} TapS installed successfully!${NC}"
echo -e "${GREEN}============================================${NC}"
echo ""
echo -e "  Version:    ${CYAN}${TAG}${NC}"
echo -e "  Arch:       ${CYAN}${ARCH}${NC}"
echo ""
echo -e "  ${CYAN}Panel${NC}"
echo -e "  Binary:     ${INSTALL_DIR}/panel"
echo -e "  Web dir:    ${WEB_DIR}"
echo -e "  Data dir:   ${PANEL_DATA}"
echo -e "  Listen:     :${PANEL_PORT}"
echo -e "  Admin:      ${ADMIN_USER}"
echo -e "  Status:     $(systemctl is-active taps-panel 2>/dev/null || echo 'unknown')"
echo ""
echo -e "  ${CYAN}Daemon${NC}"
echo -e "  Binary:     ${INSTALL_DIR}/daemon"
echo -e "  Data dir:   ${DAEMON_DATA}"
echo -e "  Listen:     ${DAEMON_ADDR}"
echo -e "  Status:     $(systemctl is-active taps-daemon 2>/dev/null || echo 'unknown')"

TOKEN_FILE="${DAEMON_DATA}/token"
if [[ -f "$TOKEN_FILE" ]]; then
  echo -e "  Token:      ${YELLOW}$(cat "$TOKEN_FILE")${NC}"
fi

FINGERPRINT=$(journalctl -u taps-daemon -n 30 --no-pager 2>/dev/null | grep -i "tls fingerprint" | tail -1 | grep -oP '(?:fingerprint:?\s*)(\S+)' | tail -1 || true)
if [[ -n "$FINGERPRINT" ]]; then
  echo -e "  TLS FP:     ${YELLOW}${FINGERPRINT}${NC}"
fi

echo ""
echo -e "  ${YELLOW}Access: http://${HOSTNAME}:${PANEL_PORT}/${NC}"
echo ""
info "Next steps:"
info "  1. Login with ${ADMIN_USER} and change the default password"
info "  2. Go to System Settings → Panel Public URL → set your external URL"
info "  3. Go to Node Management → Add → address 127.0.0.1:24445 → paste token → fetch fingerprint → save"
info "  4. Create your first instance!"
