#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

SERVICE_NAME="sentinel-go"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "🔧 Installing ChatGPT Sentinel service..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
   echo -e "${RED}Please run as root (use sudo)${NC}"
   exit 1
fi

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed. Please install Go first.${NC}"
    exit 1
fi

# Get the path to go binary
GO_PATH=$(which go)
echo -e "${GREEN}✓${NC} Found Go at: $GO_PATH"

# Create service file with proper paths
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=ChatGPT Sentinel - OpenAI-Compatible Server
Documentation=https://github.com/FreesoSaiFared/gptclient-go
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$(who am i | awk '{print $1}')
WorkingDirectory=$PROJECT_DIR
Environment="PATH=$(dirname $GO_PATH):/usr/bin:/bin"
ExecStart=$GO_PATH run ./cmd/server -config config.json -addr :7777
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

echo -e "${GREEN}✓${NC} Created service file: $SERVICE_FILE"

# Reload systemd
systemctl daemon-reload
echo -e "${GREEN}✓${NC} Reloaded systemd daemon"

# Enable service
systemctl enable "$SERVICE_NAME"
echo -e "${GREEN}✓${NC} Enabled service: $SERVICE_NAME"

# Check if config exists
if [ ! -f "$PROJECT_DIR/config.json" ]; then
    echo -e "${YELLOW}⚠${NC}  Warning: config.json not found in $PROJECT_DIR"
    echo -e "    Please run: cp $PROJECT_DIR/config.example.json $PROJECT_DIR/config.json"
    echo -e "    Then edit $PROJECT_DIR/config.json with your credentials"
fi

echo ""
echo -e "${GREEN}✓${NC} Service installed successfully!"
echo ""
echo "Next steps:"
echo "  1. Ensure config.json exists with your credentials"
echo "  2. Start the service: sudo systemctl start $SERVICE_NAME"
echo "  3. Check status: sudo systemctl status $SERVICE_NAME"
echo "  4. View logs: sudo journalctl -u $SERVICE_NAME -f"
echo ""
echo "Or use the helper scripts:"
echo "  $ ./scripts/start-sentinel.sh"
echo "  $ ./scripts/stop-sentinel.sh"
echo "  $ ./scripts/status-sentinel.sh"