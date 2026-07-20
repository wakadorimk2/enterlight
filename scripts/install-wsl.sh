#!/usr/bin/env bash
set -euo pipefail

supported_version="0.144.6"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
helper_source="${ENTERLIGHT_WSL_HELPER:-$script_dir/../dist/enterlight-linux-amd64}"
windows_binary="${ENTERLIGHT_WINDOWS_EXE:-}"

usage() {
  echo "usage: scripts/install-wsl.sh [--helper PATH] [--windows-exe PATH]" >&2
}

while (($#)); do
  case "$1" in
    --helper)
      [[ $# -ge 2 ]] || { usage; exit 2; }
      helper_source="$2"
      shift 2
      ;;
    --windows-exe)
      [[ $# -ge 2 ]] || { usage; exit 2; }
      windows_binary="$2"
      shift 2
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

if [[ -z "${WSL_INTEROP:-}" ]] || [[ "$(uname -r)" != *WSL2* && "$(uname -r)" != *microsoft-standard* ]]; then
  echo "This installer supports WSL2 only." >&2
  exit 1
fi

bin_dir="$HOME/.local/bin"
case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *)
    echo "$bin_dir is not on PATH. Add it to your shell configuration, restart the shell, then rerun this installer." >&2
    exit 1
    ;;
esac

shim_path="$bin_dir/codex"
marker="# Managed by Enterlight WSL approval adapter"
if [[ -e "$shim_path" || -L "$shim_path" ]]; then
  if [[ ! -f "$shim_path" ]] || ! head -n 2 -- "$shim_path" | grep -Fqx "$marker"; then
    echo "Refusing to overwrite existing non-Enterlight file: $shim_path" >&2
    exit 1
  fi
fi

codex_path="$(command -v codex || true)"
if [[ -z "$codex_path" ]]; then
  echo "Codex CLI was not found on PATH." >&2
  exit 1
fi
codex_path="$(readlink -f -- "$codex_path")"

if [[ "$codex_path" == "$(readlink -f -- "$shim_path" 2>/dev/null || true)" ]] || grep -Fq "$marker" -- "$codex_path" 2>/dev/null; then
  config_path="${XDG_CONFIG_HOME:-$HOME/.config}/enterlight/wsl.json"
  if [[ -f "$config_path" ]]; then
    codex_path="$(sed -n 's/^[[:space:]]*"codexPath":[[:space:]]*"\(.*\)",[[:space:]]*$/\1/p' "$config_path")"
  fi
fi
if [[ -z "$codex_path" || ! -x "$codex_path" ]]; then
  echo "Could not resolve the real Codex executable without recursing through the managed shim." >&2
  exit 1
fi

version_output="$("$codex_path" --version 2>/dev/null || true)"
if [[ ! "$version_output" =~ (^|[[:space:]])0\.144\.6($|[[:space:]]) ]]; then
  echo "Unsupported Codex CLI: ${version_output:-unknown}. This adapter requires $supported_version." >&2
  exit 1
fi

if [[ -z "$windows_binary" ]]; then
  windows_binary="$(command -v enterlight.exe || true)"
fi
if [[ -z "$windows_binary" || ! -f "$windows_binary" ]]; then
  echo "Windows enterlight.exe was not found. Pass --windows-exe /mnt/c/.../enterlight.exe." >&2
  exit 1
fi
windows_binary="$(readlink -f -- "$windows_binary")"

if [[ ! -x "$helper_source" ]]; then
  echo "WSL helper not found or not executable: $helper_source" >&2
  echo "Build it with: GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/enterlight-linux-amd64 ./cmd/enterlight-wsl" >&2
  exit 1
fi
helper_source="$(readlink -f -- "$helper_source")"
if [[ "$codex_path" == "$helper_source" ]]; then
  echo "Refusing recursive configuration: Codex resolves to the Enterlight helper." >&2
  exit 1
fi

helper_dir="$HOME/.local/lib/enterlight"
helper_path="$helper_dir/enterlight-linux-amd64"
config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/enterlight"
config_path="$config_dir/wsl.json"
mkdir -p -- "$bin_dir" "$helper_dir" "$config_dir"

install -m 0755 -- "$helper_source" "$helper_path"

json_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s' "$value"
}

config_tmp="$(mktemp "$config_dir/.wsl.json.XXXXXX")"
shim_tmp="$(mktemp "$bin_dir/.codex.XXXXXX")"
cleanup() { rm -f -- "$config_tmp" "$shim_tmp"; }
trap cleanup EXIT

printf '{\n  "codexPath": "%s",\n  "codexVersion": "%s",\n  "windowsBinary": "%s"\n}\n' \
  "$(json_escape "$codex_path")" "$supported_version" "$(json_escape "$windows_binary")" >"$config_tmp"
install -m 0600 -- "$config_tmp" "$config_path"

printf '#!/usr/bin/env sh\n%s\nexec "%s" "$@"\n' "$marker" "$helper_path" >"$shim_tmp"
install -m 0755 -- "$shim_tmp" "$shim_path"

echo "Installed Enterlight WSL adapter. Continue to run Codex as usual: codex [args...]"
