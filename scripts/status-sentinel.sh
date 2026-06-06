#!/bin/bash

SERVICE_NAME="sentinel-go"

echo "📊 ChatGPT Sentinel Service Status"
echo ""

if systemctl is-enabled "$SERVICE_NAME" &> /dev/null; then
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        echo "Status: 🟢 Running"
    else
        echo "Status: 🔴 Stopped (but enabled)"
    fi
    echo "Enabled: ✓"
else
    echo "Status: 🔴 Not installed or disabled"
    echo "Enabled: ✗"
fi

echo ""
echo "Full service status:"
echo ""

if [ "$EUID" -ne 0 ]; then 
    sudo systemctl status "$SERVICE_NAME" --no-pager -l
else
    systemctl status "$SERVICE_NAME" --no-pager -l
fi

echo ""
echo "Recent logs:"
echo ""
if [ "$EUID" -ne 0 ]; then 
    sudo journalctl -u "$SERVICE_NAME" -n 20 --no-pager
else
    journalctl -u "$SERVICE_NAME" -n 20 --no-pager
fi