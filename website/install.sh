#!/usr/bin/env bash
set -euo pipefail

REPO="AdaDigitalAgency/wp-stage-sync"
BIN="wp-stage-sync"
INSTALL_DIR="/usr/local/bin"

print_step() { printf '\033[1;34m==>\033[0m %s\n' "$1"; }
print_ok()   { printf '\033[1;32m OK\033[0m %s\n' "$1"; }
print_err()  { printf '\033[1;31mERR\033[0m %s\n' "$1" >&2; }

# Detect OS
OS="$(uname -s)"
if [[ "$OS" != "Linux" ]]; then
  print_err "Unsupported OS: $OS. Only Linux is supported."
  exit 1
fi

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *)
    print_err "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# Resolve latest release tag
print_step "Fetching latest release..."
TAG=$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' \
  | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')

if [[ -z "$TAG" ]]; then
  print_err "Could not determine latest release tag."
  exit 1
fi

URL="https://github.com/${REPO}/releases/download/${TAG}/${BIN}_linux_${ARCH}"
TMP="$(mktemp)"

print_step "Downloading ${BIN} ${TAG} (linux/${ARCH})..."
if ! curl -sSfL "$URL" -o "$TMP"; then
  print_err "Download failed: $URL"
  rm -f "$TMP"
  exit 1
fi

chmod +x "$TMP"

# Install (sudo if needed)
print_step "Installing to ${INSTALL_DIR}/${BIN}..."
if [[ -w "$INSTALL_DIR" ]]; then
  mv "$TMP" "${INSTALL_DIR}/${BIN}"
else
  sudo mv "$TMP" "${INSTALL_DIR}/${BIN}"
fi

print_ok "${BIN} ${TAG} installed successfully."
echo
echo "  Run:  wp-stage-sync            (interactive TUI)"
echo "  Run:  wp-stage-sync -u         (unattended / cron mode)"
echo "  Run:  wp-stage-sync --update   (Self update)"
echo
