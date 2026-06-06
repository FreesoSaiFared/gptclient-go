#!/bin/bash

SERVICE_NAME="sentinel-go"

echo "🗑️  Uninstalling ChatGPT Sentinel service..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
   echo "Using sudo to uninstall service..."
   SUDO="sudo"
else
   SUDO=""
fi

# Stop the service if running
if systemctl is-active --quiet "$SERVICE_NAME"; then
    echo "Stopping service..."
    $SUDO systemctl stop "$SERVICE_NAME"
fi

# Disable the service
echo "Disabling service..."
$SUDO systemctl disable "$SERVICE_NAME"

# Remove the service file
echo "Removing service file..."
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
$SUDO rm -f "$SERVICE_FILE"

# Reload systemd
echo "Reloading systemd daemon..."
$SUDO systemctl daemon-reload

# Reset failed units
$SUDO systemctl reset-failed

echo ""
echo "✓ Service uninstalled successfully"
echo ""
echo "Note: The service has been removed from systemd, but your"
echo "      project files and config.json remain in place."