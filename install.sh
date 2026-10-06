#!/bin/sh
# Installs the jinn CLI from its latest GitHub release:
#   curl -fsSL https://github.com/usejinn/jinn-cli/releases/latest/download/install.sh | sh
# This script, the binaries and SHA256SUMS are built and attested by the
# release workflow in github.com/usejinn/jinn-cli; nothing comes from another
# server. To check a file's origin: gh attestation verify FILE --repo usejinn/jinn-cli
set -eu
release=https://github.com/usejinn/jinn-cli/releases/latest/download
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) echo "jinn: no build for $(uname -m)" >&2; exit 1 ;; esac
case "$os" in linux|darwin) ;; *) echo "jinn: no build for $os; download one from https://github.com/usejinn/jinn-cli/releases" >&2; exit 1 ;; esac
file="jinn-$os-$arch"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$release/$file" -o "$tmp/jinn"
curl -fsSL "$release/SHA256SUMS" -o "$tmp/SHA256SUMS"
want=$(grep " $file\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)
if command -v sha256sum >/dev/null; then got=$(sha256sum "$tmp/jinn" | cut -d' ' -f1); else got=$(shasum -a 256 "$tmp/jinn" | cut -d' ' -f1); fi
[ -n "$want" ] && [ "$want" = "$got" ] || { echo "jinn: the download does not match SHA256SUMS" >&2; exit 1; }
chmod +x "$tmp/jinn"
dir=/usr/local/bin
[ -w "$dir" ] || dir="$HOME/.local/bin"
mkdir -p "$dir"
mv "$tmp/jinn" "$dir/jinn"
echo "jinn $("$dir/jinn" version | cut -d' ' -f2) is in $dir/jinn. Next: jinn login"
