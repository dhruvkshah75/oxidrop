#!/bin/sh
set -eu

for command in git cargo go make; do
    if ! command -v "$command" >/dev/null 2>&1; then
        echo "Error: $command is required to install Oxidrop." >&2
        exit 1
    fi
done

if [ "$(uname -s)" != Linux ]; then
    echo "Error: Oxidrop requires Linux." >&2
    exit 1
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

git clone --depth 1 --branch main https://github.com/dhruvkshah75/oxidrop.git "$tmp_dir/oxidrop"
make -C "$tmp_dir/oxidrop" build
make -C "$tmp_dir/oxidrop" install-user
printf '\nInstalled oxidrop and oxidrop-tui in %s/.local/bin.\n' "$HOME"
printf 'Ensure that directory is on your PATH, then run oxidrop-tui.\n'
