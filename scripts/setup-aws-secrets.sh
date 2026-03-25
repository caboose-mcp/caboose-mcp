#!/bin/bash
# setup-aws-secrets.sh — Populate AWS Secrets Manager with fafb runtime secrets
#
# Usage:
#   ./scripts/setup-aws-secrets.sh              # Interactive mode
#   ./scripts/setup-aws-secrets.sh --from-env   # Use environment variables

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# AWS configuration
AWS_REGION="${AWS_REGION:-us-east-1}"
SECRET_NAME="fafb/env"

echo -e "${GREEN}fafb AWS Secrets Setup${NC}"
echo "Region: $AWS_REGION"
echo "Secret: $SECRET_NAME"
echo ""

# Check AWS CLI is installed
if ! command -v aws &> /dev/null; then
    echo -e "${RED}ERROR: AWS CLI is not installed.${NC}"
    echo "Install it: https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html"
    exit 1
fi

# Check AWS credentials are configured
if ! aws sts get-caller-identity &>/dev/null; then
    echo -e "${RED}ERROR: AWS credentials are not configured.${NC}"
    echo "Run: aws configure"
    exit 1
fi

# Determine input method
FROM_ENV=false
if [[ "$1" == "--from-env" ]]; then
    FROM_ENV=true
fi

# Function to get secret value
get_secret() {
    local var_name="$1"
    local default_value="${2:-}"

    if [[ "$FROM_ENV" == true ]]; then
        # Use environment variable
        eval "echo \${$var_name:-$default_value}"
    else
        # Interactive prompt
        local current_value=""
        if [[ -n "$default_value" ]]; then
            current_value=" (default: ${default_value:0:10}...)"
        fi
        read -sp "Enter $var_name$current_value: " value
        echo ""
        if [[ -z "$value" && -n "$default_value" ]]; then
            echo "$default_value"
        else
            echo "$value"
        fi
    fi
}

# Collect secrets
if [[ "$FROM_ENV" == true ]]; then
    echo "Reading secrets from environment variables..."
    ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:-}"
    SLACK_TOKEN="${SLACK_TOKEN:-}"
    SLACK_APP_TOKEN="${SLACK_APP_TOKEN:-}"
    DISCORD_TOKEN="${DISCORD_TOKEN:-}"

    if [[ -z "$ANTHROPIC_API_KEY" ]]; then
        echo -e "${RED}ERROR: ANTHROPIC_API_KEY environment variable is not set${NC}"
        exit 1
    fi
else
    echo "Enter your AWS secrets (press Ctrl+C to cancel):"
    echo ""

    ANTHROPIC_API_KEY=$(get_secret "ANTHROPIC_API_KEY")
    SLACK_TOKEN=$(get_secret "SLACK_TOKEN")
    SLACK_APP_TOKEN=$(get_secret "SLACK_APP_TOKEN")
    DISCORD_TOKEN=$(get_secret "DISCORD_TOKEN")
    echo ""
fi

# Validate required secrets
if [[ -z "$ANTHROPIC_API_KEY" ]]; then
    echo -e "${RED}ERROR: ANTHROPIC_API_KEY is required${NC}"
    exit 1
fi

# Create or update the secret
echo "Updating AWS Secrets Manager..."
SECRET_STRING=$(jq -n \
    --arg anthropic "$ANTHROPIC_API_KEY" \
    --arg slack "$SLACK_TOKEN" \
    --arg slack_app "$SLACK_APP_TOKEN" \
    --arg discord "$DISCORD_TOKEN" \
    '{ANTHROPIC_API_KEY: $anthropic, SLACK_TOKEN: $slack, SLACK_APP_TOKEN: $slack_app, DISCORD_TOKEN: $discord}')

# Check if secret exists
if aws secretsmanager describe-secret --secret-id "$SECRET_NAME" --region "$AWS_REGION" &>/dev/null 2>&1; then
    echo "Updating existing secret: $SECRET_NAME"
    aws secretsmanager put-secret-value \
        --secret-id "$SECRET_NAME" \
        --secret-string "$SECRET_STRING" \
        --region "$AWS_REGION" > /dev/null
else
    echo "Creating new secret: $SECRET_NAME"
    aws secretsmanager create-secret \
        --name "$SECRET_NAME" \
        --description "fafb runtime secrets (ANTHROPIC_API_KEY, Discord, Slack tokens)" \
        --secret-string "$SECRET_STRING" \
        --region "$AWS_REGION" > /dev/null
fi

echo -e "${GREEN}✓ AWS Secrets updated successfully!${NC}"
echo ""
echo "Next steps:"
echo "  1. Redeploy the bots service:"
echo "     aws ecs update-service --cluster fafb --service fafb-bots --force-new-deployment"
echo ""
echo "  2. Monitor deployment:"
echo "     aws ecs wait services-stable --cluster fafb --services fafb-bots"
