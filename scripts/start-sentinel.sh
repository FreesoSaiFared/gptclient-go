#!/bin/bash

set -e

SERVICE_NAME="sentinel-go"

echo "🚀 Starting ChatGPT Sentinel service..."

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
   echo "Using sudo to start service..."
   sudo systemctl start "$SERVICE_NAME"
else
   systemctl start "$SERVICE_NAME"
fi

# Wait a moment for service to start
sleep 2

# Check if service is running
if systemctl is-active --quiet "$SERVICE_NAME"; then
    echo -e "✓ Service started successfully"
    echo ""
    echo "Service status:"
    systemctl status "$SERVICE_NAME" --no-pager -l
    echo ""
    echo "📝 View logs with:"
    echo "  sudo journalctl -u $SERVICE_NAME -f"
    echo ""
    echo "🌐 Server is running at: http://localhost:7777"
else
    echo -e "✗ Failed to start service"
    echo ""
    echo "Service status:"
    systemctl status "$SERVICE_NAME" --no-pager -l
    echo ""
    echo "📝 Check logs for errors:"
    echo "  sudo journalctl -u $SERVICE_NAME -n 50"
    exit 1
fi