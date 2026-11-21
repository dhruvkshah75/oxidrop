#!/usr/bin/env bash
set -euo pipefail

if [[ -e .env ]]; then
    echo 'A .env file already exists. Move or edit it before running setup.' >&2
    exit 1
fi

read -r -p 'Port [8080]: ' port
port=${port:-8080}
read -r -p "Storage path [$HOME/oxidrop-share]: " storage_path
storage_path=${storage_path:-$HOME/oxidrop-share}
read -r -p 'WebDAV username: ' username
read -r -s -p 'WebDAV password: ' password
echo

if [[ ! $port =~ ^[0-9]+$ ]] || (( 10#$port < 1 || 10#$port > 65535 )) || [[ -z $username || -z $password ]]; then
    echo 'Enter a valid port and a non-empty username and password.' >&2
    exit 1
fi

mkdir -p -- "$storage_path"
umask 077
# Escape values for dotenvy's double-quoted format.
quote() {
    local value=$1
    value=${value//\\/\\\\}
    value=${value//\"/\\\"}
    value=${value//\$/\\\$}
    printf '"%s"' "$value"
}
(set -o noclobber
    { printf 'PORT=%s\nSTORAGE_PATH=' "$port"; quote "$storage_path"
      printf '\nAUTH_USERNAME='; quote "$username"
      printf '\nAUTH_PASSWORD='; quote "$password"; printf '\n'; } > .env
)
echo 'WebDAV configuration saved to .env. Run oxidrop or cargo run from this directory.'
