<div align="center">

  <img src="assets/logo.png" alt="logo" width="400" height="auto" />
  
  <h1>oxidrop</h1>
  
  <p>
    <b>Bridging the Gap Between Your Desk and Your Devices</b>
  </p>

![Rust Version](https://img.shields.io/badge/rust-1.75+-E32F26.svg?style=flat&logo=rust&logoColor=white)
![Axum](https://img.shields.io/badge/axum-v0.7-blueviolet.svg?style=flat)
![Tokio](https://img.shields.io/badge/tokio-v1.36-00ADD8.svg?style=flat)
![Status](https://img.shields.io/badge/status-active-success.svg?style=flat)
![License](https://img.shields.io/badge/license-MIT-blue.svg?style=flat)
</div>

## What is Oxidrop?

Oxidrop is a fast, local file server that lets you access your Linux files from your iPad/iPhone over your local network or mobile hotspot. No cloud needed—your files stay private and under your control.

## What Problem Does It Solve?

- **No Cloud Dependency**: Direct access to your files without third-party services
- **Network Freedom**: Works over WiFi, mobile hotspot, or any local network
- **Privacy First**: Your files never leave your device
- **Fast**: Built with Rust for high-performance file streaming

## Installation

### 1. Download & Extract

Download the latest release from [GitHub Releases](https://github.com/dhruvkshah75/oxidrop/releases):

```bash
tar -xzf oxidrop-linux-x86_64.tar.gz
cd oxidrop
```

### 2. Run Setup Script

```bash
chmod +x setup.sh  # give the permission to execute 
./setup.sh
```

This will prompt you for:
- Port (default: 9090)
- Storage path (default: /home/user/oxidrop-storage)
- Username
- Password

### 3. Start the Server

```bash
./oxidrop
```

### 4. Find Your IP Address

On your Linux machine, run:

```bash
hostname -I | awk '{print $1}'
```

Or:

```bash
ip addr show | grep "inet " | grep -v 127.0.0.1 | awk '{print $2}' | cut -d/ -f1
```

Note down your IP address (e.g., `192.168.x.x`)

## Connecting from iPad/iPhone

### 1. Install WebDAV Navigator

Download **WebDAV Navigator** from the App Store

### 2. Connect to Server

1. Open WebDAV Navigator
2. Tap "+" to add a new connection
3. Enter your connection details:
   - **URL**: `http://YOUR_IP_ADDRESS:9090` (e.g., `http://192.168.1.100:9090`)
   - **Username**: The username you set during setup
   - **Password**: The password you set during setup
4. **Disable Secure Connection** 
5. Tap "Save"


