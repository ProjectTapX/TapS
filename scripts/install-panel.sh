#!/usr/bin/env bash
# TapS Panel installer — downloads latest release, sets up systemd service.
# Usage: curl -fsSL https://raw.githubusercontent.com/ProjectTapX/TapS/main/scripts/install-panel.sh | bash
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
command -v curl >/dev/null || error "curl is required. Install it first."

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
echo ""
echo -e "${CYAN}=== TapS Panel Configuration ===${NC}"
read -rp "Panel listen port [default: 24444]: " PANEL_PORT </dev/tty
PANEL_PORT="${PANEL_PORT:-24444}"
read -rp "Panel data directory [default: /var/lib/taps/panel]: " PANEL_DATA </dev/tty
PANEL_DATA="${PANEL_DATA:-/var/lib/taps/panel}"
read -rp "Web static directory [default: /opt/taps/web]: " WEB_DIR </dev/tty
WEB_DIR="${WEB_DIR:-/opt/taps/web}"
read -rp "Admin username [default: admin]: " ADMIN_USER </dev/tty
ADMIN_USER="${ADMIN_USER:-admin}"
read -rsp "Admin password [default: admin]: " ADMIN_PASS </dev/tty
ADMIN_PASS="${ADMIN_PASS:-admin}"
echo ""
echo ""

# --- Download ---
DOWNLOAD_BASE="https://github.com/${REPO}/releases/download/${TAG}"
PANEL_BIN="panel-linux-${ARCH}"

info "Downloading ${PANEL_BIN}..."
mkdir -p "${INSTALL_DIR}"
curl -fSL "${DOWNLOAD_BASE}/${PANEL_BIN}" -o "${INSTALL_DIR}/panel"
chmod +x "${INSTALL_DIR}/panel"

info "Downloading web.tar.gz..."
curl -fSL "${DOWNLOAD_BASE}/web.tar.gz" -o /tmp/taps-web.tar.gz
rm -rf "${WEB_DIR}"
mkdir -p "${WEB_DIR}"
tar -xzf /tmp/taps-web.tar.gz -C "${WEB_DIR}"
rm -f /tmp/taps-web.tar.gz

# --- Create data directory ---
mkdir -p "${PANEL_DATA}"
chmod 700 "${PANEL_DATA}"

# --- systemd unit ---
info "Creating systemd service..."
cat >/etc/systemd/system/taps-panel.service <<EOF
[Unit]
Description=TapS Panel
After=network-online.target
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
systemctl enable --now taps-panel
sleep 3

# --- Summary ---
HOSTNAME=$(hostname -I 2>/dev/null | awk '{print $1}' || hostname)
echo ""
echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN} TapS Panel installed successfully!${NC}"
echo -e "${GREEN}============================================${NC}"
echo ""
echo -e "  Version:    ${CYAN}${TAG}${NC}"
echo -e "  Arch:       ${CYAN}${ARCH}${NC}"
echo -e "  Binary:     ${CYAN}${INSTALL_DIR}/panel${NC}"
echo -e "  Web dir:    ${CYAN}${WEB_DIR}${NC}"
echo -e "  Data dir:   ${CYAN}${PANEL_DATA}${NC}"
echo -e "  Listen:     ${CYAN}:${PANEL_PORT}${NC}"
echo -e "  Admin:      ${CYAN}${ADMIN_USER}${NC}"
echo ""
echo -e "  Access:     ${YELLOW}http://${HOSTNAME}:${PANEL_PORT}/${NC}"
echo -e "  Status:     $(systemctl is-active taps-panel 2>/dev/null || echo 'unknown')"
echo ""
info "Login and change the default password immediately."
info "Then go to System Settings to configure the Panel Public URL."
