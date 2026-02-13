#!/bin/bash

# Load .env file if it exists
if [ -f .env ]; then
  export $(grep -v '^#' .env | xargs)
fi

# if $1 is "prod", PROJECT_REF is set to SUPABASE_PROJECT_REF_PROD, else to SUPABASE_PROJECT_REF_STAGING, else exit with error

if [ "$1" == "prod" ]; then
  PROJECT_REF=$SUPABASE_PROJECT_REF_PROD
elif [ "$1" == "staging" ]; then
  PROJECT_REF=$SUPABASE_PROJECT_REF_STAGING
else
  echo "Usage: $0 [prod|staging]"
  exit 1
fi

supabase link --project-ref $PROJECT_REF
