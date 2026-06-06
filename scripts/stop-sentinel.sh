#!/bin/bash

set -e

SERVICE_NAME="sentinel-go"

echo "🛑 Stopping ChatGPT Sentinel service..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
   echo "Using sudo to stop service..."
   sudo systemctl stop "$SERVICE_NAME"
else
   systemctl stop "$SERVICE_NAME"
fi

# Wait a moment for service to stop
sleep 2

# Check if service is stopped
if systemctl is-active --quiet "$SERVICE_NAME"; then
    echo "✗ Failed to stop service"
    echo ""
    echo "Service status:"
    systemctl status "$SERVICE_NAME" --no-pager -l
    exit 1
else
    echo "✓ Service stopped successfully"
    echo ""
    echo "Service status:"
    systemctl status "$SERVICE_NAME" --no-pager -l
fi