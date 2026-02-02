#!/bin/bash

# Oxidrop Configuration Script
# Creates .env file with user inputs

echo "========================================="
echo "  Oxidrop Configuration"
echo "========================================="
echo ""

# Prompt for PORT
read -p "Enter port [default: 9090]: " PORT
PORT=${PORT:-9090}

# Prompt for STORAGE_PATH
read -p "Enter storage path [default: /home/$USER/oxidrop-storage]: " STORAGE_PATH
STORAGE_PATH=${STORAGE_PATH:-/home/$USER/oxidrop-storage}

# Prompt for AUTH_USERNAME
read -p "Enter username: " AUTH_USERNAME

# Prompt for AUTH_PASSWORD
read -sp "Enter password: " AUTH_PASSWORD
echo ""

# Create storage directory
mkdir -p "$STORAGE_PATH"

# Create .env file
cat > .env << ENVEOF
PORT=$PORT
HOST_ADDR=0.0.0.0
STORAGE_PATH=$STORAGE_PATH
AUTH_USERNAME=$AUTH_USERNAME
AUTH_PASSWORD=$AUTH_PASSWORD
ENVEOF

echo ""
echo "✓ Configuration saved to .env"
echo "✓ Storage directory created: $STORAGE_PATH"
echo ""
echo "Run: ./oxidrop"
echo ""
