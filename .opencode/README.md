# ChatGPT Sentinel - opencode Provider Configuration

This project includes opencode provider configuration for "ChatGPT Sentinel", which allows opencode to use this ChatGPT web client as an AI provider.

## Provider Details

**Provider Name:** `chatgpt-sentinel`
**Base URL:** `http://localhost:7777/v1`
**API Key:** `sk-sentinel-local` (dummy key, uses local config.json)

## Available Models

- `gpt-5-5-thinking` - Enhanced reasoning model (default)
- `gpt-5-5` - Base GPT-5.5 model
- `o4-mini-high` - High performance O4 Mini model

## Configuration Location

The provider configuration is stored in `.opencode/opencode.json` at the project root.

## Usage

After starting the service, you can use the provider in opencode:

```bash
# Start the service first
sudo ./scripts/start-sentinel.sh

# Then use the model in opencode
opencode --model chatgpt-sentinel/gpt-5-5-thinking
```

Or in your opencode configuration:

```json
{
  "model": "chatgpt-sentinel/gpt-5-5-thinking"
}
```

## Integration with Service

The provider is designed to work with the systemd service:

1. Install and start the service: `sudo ./scripts/install-service.sh`
2. The service will automatically start the OpenAI-compatible server on port 7777
3. opencode can then connect to `http://localhost:7777/v1`

## Testing the Provider

Test the provider directly with curl:

```bash
curl -X POST http://localhost:7777/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-sentinel-local" \
  -d '{
    "model": "gpt-5-5-thinking",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": false
  }'
```

## Requirements

- Service must be running on port 7777
- Valid config.json with ChatGPT credentials
- Network access to ChatGPT servers
- opencode with OpenAI-compatible provider support