#!/bin/sh
# Stillroom installer.
#
#   curl -fsSL https://stillroom.sh/install.sh | sh
#
# Downloads the release binary for this platform, VERIFIES ITS CHECKSUM against
# the signed checksums.txt from the same release, and installs it. If you would
# rather read it first — which is the right instinct for anything you pipe into
# a shell — download it, read it, then run it:
#
#   curl -fsSLO https://stillroom.sh/install.sh && less install.sh && sh install.sh
#
# Environment:
#   STILLROOM_VERSION   install a specific tag instead of the latest release
#   STILLROOM_PREFIX    install root (default: ~/.local, or /usr/local if writable)

set -eu

REPO="A4ever369/Stillroom"
BIN="still"

say()  { printf '%s\n' "$*"; }
warn() { printf '%s\n' "$*" >&2; }
die()  { printf '\nerror: %s\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "this installer needs \`$1\` and could not find it."
}

need uname
need tar
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1"; }
  fetch_to() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO- "$1"; }
  fetch_to() { wget -qO "$2" "$1"; }
else
  die "this installer needs curl or wget."
fi

# ---- platform ----

os=$(uname -s)
case "$os" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  MINGW*|MSYS*|CYGWIN*)
    die "Windows is not supported by this installer yet.
Grab the .zip from https://github.com/$REPO/releases and put still.exe on your PATH." ;;
  *) die "unsupported operating system: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

# ---- version ----

version="${STILLROOM_VERSION:-}"
if [ -z "$version" ]; then
  version=$(fetch "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1) || true
fi
if [ -z "$version" ]; then
  die "could not find a published release of Stillroom.

If you are reading this before the first release is cut, install from source:
  go install github.com/$REPO/cmd/$BIN@latest"
fi
num=${version#v}

# ---- download and verify ----

tmp=$(mktemp -d)
# Clean up on any exit, including the failure paths below.
trap 'rm -rf "$tmp"' EXIT INT TERM

archive="${BIN}_${num}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

say "Stillroom $version — $os/$arch"
fetch_to "$base/$archive" "$tmp/$archive" \
  || die "could not download $archive.
Check https://github.com/$REPO/releases for what this release actually publishes."

# A binary that installs itself onto your machine should prove it is the one
# the release published. Missing checksums are a hard stop, not a warning.
fetch_to "$base/checksums.txt" "$tmp/checksums.txt" \
  || die "could not download checksums.txt — refusing to install unverified bytes."

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
else
  die "neither sha256sum nor shasum is available — refusing to install unverified bytes."
fi

expected=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1 | head -n1)
[ -n "$expected" ] || die "$archive is not listed in checksums.txt — refusing to install."
[ "$actual" = "$expected" ] || die "checksum mismatch for $archive.
  expected $expected
  got      $actual
Do not use this download."

say "checksum ok"
tar -xzf "$tmp/$archive" -C "$tmp"
[ -f "$tmp/$BIN" ] || die "the archive did not contain $BIN."

# ---- install ----

prefix="${STILLROOM_PREFIX:-}"
if [ -z "$prefix" ]; then
  if [ -w /usr/local/bin ] 2>/dev/null; then
    prefix=/usr/local
  else
    prefix="$HOME/.local"
  fi
fi
dest="$prefix/bin"
mkdir -p "$dest" || die "cannot create $dest"
install -m 0755 "$tmp/$BIN" "$dest/$BIN" 2>/dev/null \
  || { cp "$tmp/$BIN" "$dest/$BIN" && chmod 0755 "$dest/$BIN"; } \
  || die "cannot write $dest/$BIN.
Try: STILLROOM_PREFIX=\$HOME/.local sh install.sh"

say "installed $dest/$BIN"

case ":$PATH:" in
  *":$dest:"*) ;;
  *)
    warn ""
    warn "$dest is not on your PATH. Add it:"
    warn "    export PATH=\"$dest:\$PATH\""
    ;;
esac

say ""
say "next:  still publish     share what you learned as a link"
say "       still pull LINK   receive what someone else learned"
