#!/bin/bash

set -e

# Parse arguments
QUERY=""
FILE=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --query)
            QUERY="$2"
            shift 2
            ;;
        --file)
            FILE="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 --query 'SELECT ...' OR --file path/to/file.sql"
            exit 1
            ;;
    esac
done

if [ -z "$QUERY" ] && [ -z "$FILE" ]; then
    echo "Error: Must provide either --query or --file"
    echo "Usage: $0 --query 'SELECT ...' OR --file path/to/file.sql"
    exit 1
fi

if [ -n "$QUERY" ] && [ -n "$FILE" ]; then
    echo "Error: Cannot use both --query and --file"
    exit 1
fi

if [ -n "$FILE" ] && [ ! -f "$FILE" ]; then
    echo "Error: File not found: $FILE"
    exit 1
fi

# Read env vars from .env file
if [ -f .env ]; then
    export $(grep -v '^#' .env | xargs)
fi

CONNECTION_STRING=""

echo "Select environment to connect to:"
echo "1. PROD"
echo "2. STAGING"
echo ""
read -p "> " ENVIRONMENT

if [ "$ENVIRONMENT" == "1" ]; then
    CONNECTION_STRING="postgresql://postgres.${SUPABASE_PROJECT_REF_PROD}"
elif [ "$ENVIRONMENT" == "2" ]; then
    CONNECTION_STRING="postgresql://postgres.${SUPABASE_PROJECT_REF_STAGING}"
else
    echo "Invalid selection"
    exit 1
fi

DB_URL="$CONNECTION_STRING:${SUPABASE_PW}@${SUPABASE_AWS_REGION}:${SUPABASE_PORT}/postgres"

if [ -n "$QUERY" ]; then
    psql "$DB_URL" -c "$QUERY"
else
    psql "$DB_URL" -f "$FILE"
fi
