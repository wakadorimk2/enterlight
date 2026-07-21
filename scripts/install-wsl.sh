#!/usr/bin/env bash
set -euo pipefail

supported_version="0.144.6"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
helper_source="${ENTERLIGHT_WSL_HELPER:-$script_dir/../dist/enterlight-linux-amd64}"
windows_binary="${ENTERLIGHT_WINDOWS_EXE:-}"
install_codex_hooks=false

usage() {
  echo "usage: scripts/install-wsl.sh [--helper PATH] [--windows-exe PATH] [--install-codex-hooks]" >&2
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
    --install-codex-hooks)
      install_codex_hooks=true
      shift
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

path_precedes() {
  local earlier="$1"
  local later="$2"
  local entry
  local -a entries
  IFS=: read -r -a entries <<<"$PATH"
  for entry in "${entries[@]}"; do
    [[ -n "$entry" ]] || entry="."
    [[ "$entry" == "$earlier" ]] && return 0
    [[ "$entry" == "$later" ]] && return 1
  done
  return 1
}

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
codex_command_dir="$(dirname -- "$codex_path")"
if [[ "$codex_command_dir" != "$bin_dir" ]] && ! path_precedes "$bin_dir" "$codex_command_dir"; then
  echo "$bin_dir appears after $codex_command_dir on PATH, so the managed shim would not run." >&2
  echo "Move $bin_dir before $codex_command_dir in your shell configuration, restart the shell, then rerun this installer." >&2
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
hook_wrapper="$helper_dir/codex-hook-wsl"
config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/enterlight"
config_path="$config_dir/wsl.json"
hooks_path="$HOME/.codex/hooks.json"
hook_marker="# Managed by Enterlight WSL Codex lifecycle hooks"
mkdir -p -- "$bin_dir" "$helper_dir" "$config_dir"

if $install_codex_hooks && [[ -e "$hook_wrapper" || -L "$hook_wrapper" ]]; then
  if [[ ! -f "$hook_wrapper" ]] || ! head -n 2 -- "$hook_wrapper" | grep -Fqx "$hook_marker"; then
    echo "Refusing to overwrite existing non-Enterlight file: $hook_wrapper" >&2
    exit 1
  fi
fi

install -m 0755 -- "$helper_source" "$helper_path"

json_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s' "$value"
}

config_tmp="$(mktemp "$config_dir/.wsl.json.XXXXXX")"
shim_tmp="$(mktemp "$bin_dir/.codex.XXXXXX")"
hook_tmp="$(mktemp "$helper_dir/.codex-hook-wsl.XXXXXX")"
cleanup() { rm -f -- "$config_tmp" "$shim_tmp" "$hook_tmp"; }
trap cleanup EXIT

hooks_managed=false
if $install_codex_hooks || { [[ -f "$config_path" ]] && grep -Eq '"codexHooksManaged"[[:space:]]*:[[:space:]]*true' "$config_path"; }; then
  hooks_managed=true
fi

if $hooks_managed; then
  printf '{\n  "codexPath": "%s",\n  "codexVersion": "%s",\n  "windowsBinary": "%s",\n  "codexHooksManaged": true\n}\n' \
    "$(json_escape "$codex_path")" "$supported_version" "$(json_escape "$windows_binary")" >"$config_tmp"
else
  printf '{\n  "codexPath": "%s",\n  "codexVersion": "%s",\n  "windowsBinary": "%s"\n}\n' \
    "$(json_escape "$codex_path")" "$supported_version" "$(json_escape "$windows_binary")" >"$config_tmp"
fi

if $install_codex_hooks; then
  windows_command="$(wslpath -w -- "$windows_binary")"
  printf '#!/usr/bin/env bash\n%s\nset -u\n\nif (($# != 1)); then\n  exit 2\nfi\ncase "$1" in\n  working|waiting|done|error|off) state="$1" ;;\n  *) exit 2 ;;\nesac\n\nwindows_binary=%q\n/usr/bin/timeout --kill-after=1s 3s powershell.exe -NoProfile -NonInteractive -Command '\''& $args[0] codex-hook $args[1]'\'' "$windows_binary" "$state" </dev/null >/dev/null 2>&1 || true\nexit 0\n' \
    "$hook_marker" "$windows_command" >"$hook_tmp"
  install -m 0755 -- "$hook_tmp" "$hook_wrapper"
  ENTERLIGHT_WSL_INTERNAL_MANAGE_HOOKS=1 "$helper_path" install-codex-hooks "$hooks_path" "$hook_wrapper" "$windows_command"
fi

install -m 0600 -- "$config_tmp" "$config_path"

printf '#!/usr/bin/env sh\n%s\nexec "%s" "$@"\n' "$marker" "$helper_path" >"$shim_tmp"
install -m 0755 -- "$shim_tmp" "$shim_path"

echo "Installed Enterlight WSL adapter. Continue to run Codex as usual: codex [args...]"
