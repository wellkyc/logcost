#!/bin/sh
set -eu

repo="wellkyc/logcost"
dir="${LOGCOST_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=macos ;;
    *)
        echo "logcost: $(uname -s) is not supported by this script, on Windows use install.ps1" >&2
        exit 1
        ;;
esac

case "$(uname -m)" in
    x86_64 | amd64) arch=x86_64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *)
        echo "logcost: no prebuilt binary for $(uname -m), try: go install github.com/$repo/cmd/logcost@latest" >&2
        exit 1
        ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
    hash_file() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
    hash_file() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
    echo "logcost: need sha256sum or shasum to check the download" >&2
    exit 1
fi

archive="logcost-$os-$arch.tar.gz"
base="https://github.com/$repo/releases/latest/download"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $base/$archive"
curl -fsSL --proto '=https' --tlsv1.2 "$base/$archive" -o "$tmp/$archive"
curl -fsSL --proto '=https' --tlsv1.2 "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"

expected="$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/SHA256SUMS")"
actual="$(hash_file "$tmp/$archive")"
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
    echo "logcost: checksum mismatch for $archive, not installing" >&2
    exit 1
fi
echo "Checksum OK"

tar -xzf "$tmp/$archive" -C "$tmp" logcost
mkdir -p "$dir"
mv "$tmp/logcost" "$dir/logcost"
chmod 755 "$dir/logcost"
echo "Installed logcost to $dir/logcost"

case ":$PATH:" in
    *":$dir:"*) ;;
    *)
        echo
        echo "$dir is not in your PATH yet. Add it with:"
        echo "  echo 'export PATH=\"$dir:\$PATH\"' >> ~/.profile"
        ;;
esac
