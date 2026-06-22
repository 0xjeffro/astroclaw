#!/bin/bash
# Interactive deployment script for AstroClaw.
# Guides the user through CDK deployment and generates the run command.
set -e

INFRA_DIR="$(cd "$(dirname "$0")/../deploy/aws/infra" && pwd)"
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUTS_FILE="$INFRA_DIR/cdk-outputs.json"

# Ask a yes/no question. Loops until the user enters y or n.
ask_yn() {
  local prompt="$1"
  while true; do
    read -p "$prompt (y/n): " answer
    case "$answer" in
      y|Y) return 0 ;;
      n|N) return 1 ;;
      *) echo "Please enter y or n." ;;
    esac
  done
}

echo "=== AstroClaw Deploy ==="
echo ""

# Step 1: First deploy?
GENERATE_API_KEY="false"
GENERATE_ADMIN_PASSWORD="false"
if ask_yn "Is this your first deploy?"; then
  GENERATE_API_KEY="true"
  GENERATE_ADMIN_PASSWORD="true"
else
  if ask_yn "Do you want to rotate the API key?"; then
    GENERATE_API_KEY="true"
  fi
  if ask_yn "Do you want to rotate the admin password?"; then
    GENERATE_ADMIN_PASSWORD="true"
  fi
fi

# Step 2: Anthropic API key
read -p "Anthropic API key (leave empty to keep existing): " ANTHROPIC_KEY

# Step 3: Build CDK deploy command
CDK_ARGS="--parameters GenerateApiKey=$GENERATE_API_KEY --parameters GenerateAdminPassword=$GENERATE_ADMIN_PASSWORD"
if [ -n "$ANTHROPIC_KEY" ]; then
  CDK_ARGS="$CDK_ARGS --parameters AnthropicApiKey=$ANTHROPIC_KEY"
fi

echo ""
echo "--- Deploy Config ---"
echo "GenerateApiKey:        $GENERATE_API_KEY"
echo "GenerateAdminPassword: $GENERATE_ADMIN_PASSWORD"
if [ -n "$ANTHROPIC_KEY" ]; then
  echo "AnthropicApiKey:       (provided)"
else
  echo "AnthropicApiKey:       (keep existing)"
fi
echo "---------------------"
echo ""
if ! ask_yn "Proceed with deployment?"; then
  echo "Aborted."
  exit 0
fi

# Step 4: Deploy
echo ""
echo "Deploying..."
cd "$INFRA_DIR"
npx cdk deploy $CDK_ARGS --outputs-file "$OUTPUTS_FILE"

# Step 5: Extract outputs and print run command
API_URL=$(jq -r '.InfraStack.ApiUrl' "$OUTPUTS_FILE")
REPLY_URL=$(jq -r '.InfraStack.ReplyUrl' "$OUTPUTS_FILE")
WS_URL=$(jq -r '.InfraStack.WebSocketUrl' "$OUTPUTS_FILE")
API_KEY=$(jq -r '.InfraStack.GeneratedApiKey // empty' "$OUTPUTS_FILE")
ADMIN_PASSWORD=$(jq -r '.InfraStack.GeneratedAdminPassword // empty' "$OUTPUTS_FILE")

# If no API key in outputs (GenerateApiKey=false), ask user to provide it.
if [ -z "$API_KEY" ]; then
  echo ""
  echo "No new API key generated (GenerateApiKey=false)."
  read -p "Enter your existing API key: " API_KEY
fi

echo ""
echo "=== Deploy Complete ==="
echo ""
echo "API URL:       $API_URL"
echo "Reply URL:     $REPLY_URL"
echo "WebSocket URL: $WS_URL"
echo "API Key:       $API_KEY"
if [ -n "$ADMIN_PASSWORD" ]; then
  echo "Admin Password: $ADMIN_PASSWORD"
  echo "  (SAVE THIS NOW. Only shown on this deploy.)"
fi
echo ""
echo "--- Run TUI ---"
echo ""
echo "API_URL=$API_URL \\"
echo "REPLY_URL=$REPLY_URL \\"
echo "WS_URL=$WS_URL \\"
echo "API_KEY=$API_KEY \\"
echo "go run ./tui/"
echo ""
