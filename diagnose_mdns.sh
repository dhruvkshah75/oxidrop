#!/bin/bash

echo "==================================="
echo "Oxidrop mDNS Diagnostic Tool"
echo "==================================="

# Get the port from .env
PORT=$(grep "^PORT=" .env | cut -d= -f2)
echo "Port configured: $PORT"

echo -e "\n1. Testing hostname resolution..."
if ping -c 1 -W 2 oxidrop.local &>/dev/null; then
    IP=$(ping -c 1 oxidrop.local | grep -oP '\(\K[0-9.]+(?=\))')
    echo "✓ oxidrop.local resolves to: $IP"
else
    echo "✗ oxidrop.local does NOT resolve"
    exit 1
fi

echo -e "\n2. Checking if server is running..."
if lsof -i :$PORT &>/dev/null; then
    echo "✓ Server is running on port $PORT"
else
    echo "✗ No server running on port $PORT"
    echo "  Start the server with: cargo run"
    exit 1
fi

echo -e "\n3. Testing HTTP connection with IP address..."
if curl -s -u "DhruvShah:Kthek#123456" "http://$IP:$PORT/ping" &>/dev/null; then
    echo "✓ HTTP works with IP: http://$IP:$PORT"
else
    echo "✗ HTTP failed with IP address"
fi

echo -e "\n4. Testing HTTP connection with oxidrop.local..."
if curl -s -u "DhruvShah:Kthek#123456" "http://oxidrop.local:$PORT/ping" &>/dev/null; then
    echo "✓ HTTP works with hostname: http://oxidrop.local:$PORT"
else
    echo "✗ HTTP failed with hostname"
    echo "  Check DNS resolution and firewall"
fi

echo -e "\n5. Checking avahi-daemon status..."
if systemctl is-active --quiet avahi-daemon 2>/dev/null; then
    echo "✓ avahi-daemon is running"
else
    echo "⚠ avahi-daemon might not be running"
fi

echo -e "\n6. Network information..."
echo "Your IP addresses:"
ip -4 addr show | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | grep -v 127.0.0.1

echo -e "\n==================================="
echo "Diagnostic complete!"
echo "==================================="
echo ""
echo "If everything shows ✓ but browser doesn't work:"
echo "  - Try clearing browser cache"
echo "  - Use incognito/private mode"
echo "  - Try: http://oxidrop.local:$PORT (not https://)"
echo "  - If on another device, ensure same network"
echo ""
