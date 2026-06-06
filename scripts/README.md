# Service Installation Scripts

This directory contains scripts to install and manage the ChatGPT Sentinel service.

## Scripts

- `install-service.sh` - Install the service as a systemd service
- `start-sentinel.sh` - Start the service
- `stop-sentinel.sh` - Stop the service  
- `status-sentinel.sh` - Check service status and logs
- `uninstall-service.sh` - Remove the service from systemd

## Quick Start

1. **Install the service:**
   ```bash
   sudo ./scripts/install-service.sh
   ```

2. **Start the service:**
   ```bash
   sudo ./scripts/start-sentinel.sh
   ```

3. **Check status:**
   ```bash
   ./scripts/status-sentinel.sh
   ```

4. **View logs:**
   ```bash
   sudo journalctl -u sentinel-go -f
   ```

5. **Stop the service:**
   ```bash
   sudo ./scripts/stop-sentinel.sh
   ```

## Manual Systemctl Commands

You can also use systemctl directly:

```bash
# Start
sudo systemctl start sentinel-go

# Stop
sudo systemctl stop sentinel-go

# Restart
sudo systemctl restart sentinel-go

# Enable on boot
sudo systemctl enable sentinel-go

# Check status
sudo systemctl status sentinel-go

# View logs
sudo journalctl -u sentinel-go -f
```

## Service Configuration

The service is configured to:
- Start on boot (multi-user.target)
- Restart automatically on failure
- Listen on port 7777
- Use config.json in the project directory
- Log to systemd journal

## Requirements

- systemd (standard on most Linux distributions)
- Go runtime installed and in PATH
- config.json with valid credentials
- Network access to ChatGPT

## Troubleshooting

**Service won't start:**
```bash
sudo journalctl -u sentinel-go -n 50
```

**Config issues:**
```bash
# Check if config exists
ls -la config.json

# Validate config
cat config.json
```

**Permission issues:**
```bash
# Ensure service user can read config
chmod 600 config.json

# Check service user
grep "^User" /etc/systemd/system/sentinel-go.service
```