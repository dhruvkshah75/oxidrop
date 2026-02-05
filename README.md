<div align="center">

  <img src="assets/logo.png" alt="logo" width="400" height="auto" />
  
  <h1>oxidrop</h1>
  
  <p>
    <b>Bridging the Gap Between Your Desk and Your Devices</b>
  </p>

  <a href="https://www.rust-lang.org/">
    <img src="https://img.shields.io/badge/Language-Rust-A72145?style=flat&logo=rust&logoColor=white" alt="Written in Rust">
  </a>
    <img src="https://img.shields.io/badge/status-active-success?style=flat" alt="Status Active">
    <img alt="issues" src="https://img.shields.io/github/issues/dhruvkshah75/oxidrop" >
    <img alt="license" src="https://img.shields.io/github/license/dhruvkshah75/oxidrop">
  <a href="https://github.com/dhruvkshah75/oxidrop/releases/tag/v1.0.1">
    <img alt="version" src="https://img.shields.io/github/v/release/dhruvkshah75/oxidrop">
  </a>
  <br>
  <br>
  <p>
      <a href="https://www.producthunt.com/posts/oxidrop" target="_blank"><img src="https://api.producthunt.com/widgets/embed-image/v1/featured.svg?post_id=288767&theme=dark" alt="Oxidrop-Blazing fast wireless file sync | Product Hunt" style="width: 250px; height: 54px;" width="250" height="54" /></a>
  </p>
  <br>
</div>

## What is Oxidrop?

Oxidrop is a fast, local file server that lets you access your Linux files from your iPad/iPhone over your local network or mobile hotspot. No cloud needed—your files stay private and under your control.

## What Problem Does It Solve?

- **No Cloud Dependency**: Direct access to your files without third-party services
- **Network Freedom**: Works over WiFi, mobile hotspot, or any local network
- **Privacy First**: Your files never leave your device
- **Fast**: Built with Rust for high-performance file streaming

## Table of Contents

- [Demo](#demo)
- [Installation](#installation)
- [Connecting](#connecting)


## Demo
![DemoRecording](./assets/output.gif)
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

# change the port depeneding on the port that u set 
sudo ufw allow 8080/tcp # allow traffic on port 8080 
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

## Connecting
How to connect mobile device like iPad, iPhone, Android devices to the server 
### 1. Install WebDAV Nav or OwlFiles

Download **WebDAV Nav** from the App Store or **OwlFiles** from the playstore

### 2. Connect to Server

1. Open WebDAV Navigator
2. Tap "+" to add a new connection
3. Enter your connection details:
   - **URL**: `http://oxidrop.local:<port>/`  (default: `http://oxidrop.local:9090/`)
   - **Username**: The username you set during setup
   - **Password**: The password you set during setup
4. **Disable Secure Connection** 
5. Tap "Save"


