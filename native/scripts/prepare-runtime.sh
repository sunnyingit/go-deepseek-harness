#!/usr/bin/env bash
# Download portable Python + Node, vendor pip wheels, copy skills, and
# compile the web backend into native/runtime/ for electron-builder extraResources.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
NATIVE="$ROOT/native"
DEST="${DEST:-$NATIVE/runtime}"
# 0 = keep downloaded archives for reuse (default). 1 / --prune-archives = delete them after extract.
PRUNE_ARCHIVES="${PRUNE_ARCHIVES:-0}"

TARGET=""
for arg in "$@"; do
  case "$arg" in
    --prune-archives) PRUNE_ARCHIVES=1 ;;
    darwin-arm64|darwin-x64|win-x64) TARGET="$arg" ;;
    -h|--help)
      echo "usage: $0 [--prune-archives] [darwin-arm64|darwin-x64|win-x64]" >&2
      echo "  --prune-archives  delete .tar.gz/.zip after extract (or PRUNE_ARCHIVES=1)" >&2
      exit 0
      ;;
    *)
      echo "usage: $0 [--prune-archives] [darwin-arm64|darwin-x64|win-x64]" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$TARGET" ]]; then
  case "$(uname -s)-$(uname -m)" in
    Darwin-arm64) TARGET=darwin-arm64 ;;
    Darwin-x86_64) TARGET=darwin-x64 ;;
    MINGW*|MSYS*|CYGWIN*) TARGET=win-x64 ;;
    *)
      echo "usage: $0 [--prune-archives] [darwin-arm64|darwin-x64|win-x64]" >&2
      exit 1
      ;;
  esac
fi

prune_archives() {
  if [[ "$PRUNE_ARCHIVES" == 1 ]]; then
    rm -f "$@"
  fi
}

NODE_VERSION="${NODE_VERSION:-22.14.0}"
# python-build-standalone: keep these two in lockstep (see GitHub releases).
PYTHON_RELEASE="${PYTHON_RELEASE:-20260814}"
PYTHON_VER="${PYTHON_VER:-3.12.14}"
MINGIT_VERSION="${MINGIT_VERSION:-2.51.0}"
MINGIT_TAG="${MINGIT_TAG:-v2.51.0.windows.1}"
PACKAGES=(python-docx python-pptx openpyxl pypdf reportlab pandas Pillow)

mkdir -p "$DEST/bin" "$DEST/wheels"
cd "$DEST"

# Archives we fetch here are tens of MB. Resume partial files instead of
# treating a leftover from a killed curl as a finished cache hit.
min_archive_bytes=5000000

download() {
  local url="$1" out="$2"
  echo "GET $url"
  curl -fL -C - --retry 5 --retry-all-errors --retry-delay 2 --connect-timeout 20 --max-time 1800 -o "$out" "$url"
  local sz
  sz=$(wc -c < "$out" | tr -d ' ')
  if [[ "$sz" -lt "$min_archive_bytes" ]]; then
    echo "download incomplete: $out ($sz bytes) from $url (kept for resume)" >&2
    exit 1
  fi
}

case "$TARGET" in
  darwin-arm64)
    NODE_DIST="node-v${NODE_VERSION}-darwin-arm64"
    NODE_URL="https://nodejs.org/dist/v${NODE_VERSION}/${NODE_DIST}.tar.gz"
    PY_TRIPLE="aarch64-apple-darwin"
    GOOS=darwin GOARCH=arm64
    WEB_NAME=pabuddy-web
    PIP_PLAT=macosx_11_0_arm64
    ;;
  darwin-x64)
    NODE_DIST="node-v${NODE_VERSION}-darwin-x64"
    NODE_URL="https://nodejs.org/dist/v${NODE_VERSION}/${NODE_DIST}.tar.gz"
    PY_TRIPLE="x86_64-apple-darwin"
    GOOS=darwin GOARCH=amd64
    WEB_NAME=pabuddy-web
    PIP_PLAT=macosx_11_0_x86_64
    ;;
  win-x64)
    NODE_DIST="node-v${NODE_VERSION}-win-x64"
    NODE_URL="https://nodejs.org/dist/v${NODE_VERSION}/${NODE_DIST}.zip"
    PY_TRIPLE="x86_64-pc-windows-msvc"
    GOOS=windows GOARCH=amd64
    WEB_NAME=pabuddy-web.exe
    PIP_PLAT=win_amd64
    ;;
  *)
    echo "unknown target $TARGET" >&2
    exit 1
    ;;
esac

PY_ARCHIVE="cpython-${PYTHON_VER}+${PYTHON_RELEASE}-${PY_TRIPLE}-install_only_stripped.tar.gz"
PY_URL="https://github.com/astral-sh/python-build-standalone/releases/download/${PYTHON_RELEASE}/${PY_ARCHIVE}"

# --- Node ---
if [[ ! -x node/bin/node && ! -f node/node.exe ]]; then
  rm -rf node "$NODE_DIST"
  if [[ "$NODE_URL" == *.zip ]]; then
    download "$NODE_URL" "${NODE_DIST}.zip"
    unzip -q "${NODE_DIST}.zip"
  else
    download "$NODE_URL" "${NODE_DIST}.tar.gz"
    tar -xzf "${NODE_DIST}.tar.gz"
  fi
  mv "$NODE_DIST" node
  prune_archives "${NODE_DIST}.tar.gz" "${NODE_DIST}.zip"
fi
echo "node: $(ls node/bin/node node/node.exe 2>/dev/null | head -1)"

# --- Python ---
if [[ ! -x python/bin/python3 && ! -f python/python.exe ]]; then
  rm -rf python
  download "$PY_URL" "$PY_ARCHIVE"
  tar -xzf "$PY_ARCHIVE"
  if [[ ! -d python ]]; then
    echo "python archive did not contain python/" >&2
    exit 1
  fi
  prune_archives "$PY_ARCHIVE"
fi
if [[ "$(uname -s)" == Darwin ]]; then
  xattr -cr python node 2>/dev/null || true
  chmod -R u+w python node
fi
echo "python: $(ls python/bin/python3 python/python.exe 2>/dev/null | head -1)"

# --- Git Bash (Windows only; macOS already has /bin/bash) ---
if [[ "$TARGET" == win-x64 ]]; then
  if [[ ! -f git/usr/bin/bash.exe && ! -f git/bin/bash.exe ]]; then
    rm -rf git
    MINGIT_ZIP="MinGit-${MINGIT_VERSION}-64-bit.zip"
    download "https://github.com/git-for-windows/git/releases/download/${MINGIT_TAG}/${MINGIT_ZIP}" "$MINGIT_ZIP"
    mkdir -p git
    unzip -q "$MINGIT_ZIP" -d git
  fi
  echo "git bash: $(ls git/usr/bin/bash.exe git/bin/bash.exe 2>/dev/null | head -1)"
fi

# Prefer the bundled interpreter when it can actually run on this machine.
BUNDLED_PY=""
if [[ -x python/bin/python3 && "$TARGET" != win-x64 ]]; then
  BUNDLED_PY="$DEST/python/bin/python3"
elif [[ -f python/python.exe && "$TARGET" == win-x64 && "$(uname -s)" != Darwin ]]; then
  BUNDLED_PY="$DEST/python/python.exe"
fi

HOST_PY="${HOST_PY:-python3}"
if ! command -v "$HOST_PY" >/dev/null 2>&1; then
  HOST_PY=""
fi
PIP_PY="${BUNDLED_PY:-$HOST_PY}"

# --- wheels (so a Windows first launch, or a cache miss, can pip without PyPI) ---
if [[ -n "$PIP_PY" ]]; then
  if compgen -G "$DEST/wheels/*.whl" >/dev/null 2>&1; then
    echo "wheels already present in $DEST/wheels"
  else
    echo "downloading wheels with $PIP_PY"
    "$PIP_PY" -m ensurepip --upgrade >/dev/null 2>&1 || true
    if [[ -n "$BUNDLED_PY" ]]; then
      "$PIP_PY" -m pip download -d "$DEST/wheels" "${PACKAGES[@]}" \
        || echo "wheel download failed (first launch will use the network)"
    else
      "$PIP_PY" -m pip download -d "$DEST/wheels" --only-binary=:all: \
        --python-version 312 --platform "$PIP_PLAT" "${PACKAGES[@]}" \
        || echo "cross wheel download failed (first launch will use the network)"
    fi
  fi
else
  echo "no python to vendor wheels; first launch will pip install from the network"
fi

# Install document packages into the relocatable interpreter when we can
# execute it. EnsureEnv then reuses python/.packages.json and skips venv.
if [[ -n "$BUNDLED_PY" ]]; then
  if [[ -f "$DEST/python/.packages.json" ]]; then
    echo "python already preinstalled ($(cat "$DEST/python/.packages.json"))"
  else
    echo "pip install into bundled python"
    "$BUNDLED_PY" -m pip install --upgrade pip
    "$BUNDLED_PY" -m pip install --find-links "$DEST/wheels" "${PACKAGES[@]}"
    "$BUNDLED_PY" -c 'import json, pathlib, sys; pathlib.Path(sys.argv[1]).write_text(json.dumps(sorted(sys.argv[2:])))' \
      "$DEST/python/.packages.json" "${PACKAGES[@]}"
    echo "preinstalled marker: python/.packages.json"
  fi
fi

# --- skills + config ---
rm -rf "$DEST/skills"
mkdir -p "$DEST/skills"
if command -v rsync >/dev/null 2>&1; then
  rsync -a --exclude node_modules --exclude .git "$ROOT/web/skills/" "$DEST/skills/"
else
  cp -R "$ROOT/web/skills/." "$DEST/skills/"
fi
cp "$ROOT/web/config.yaml" "$DEST/config.yaml"

# --- web binary ---
echo "go build GOOS=$GOOS GOARCH=$GOARCH -> bin/$WEB_NAME"
( cd "$ROOT" && CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -o "$DEST/bin/$WEB_NAME" ./web )
chmod +x "$DEST/bin/$WEB_NAME" 2>/dev/null || true

# Archives stay on disk by default so the next build can reuse them.
# They are already excluded from the .app via extraResources filter.
if [[ "$PRUNE_ARCHIVES" == 1 ]]; then
  rm -f "$DEST"/*.tar.gz "$DEST"/*.zip
fi

if [[ "$(uname -s)" == Darwin ]]; then
  xattr -cr "$DEST" 2>/dev/null || true
fi

echo "runtime ready at $DEST ($TARGET)"
test -e "$DEST/bin/$WEB_NAME"
test -d "$DEST/python"
test -d "$DEST/node"
test -d "$DEST/skills"
