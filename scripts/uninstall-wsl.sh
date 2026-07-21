#!/usr/bin/env bash
set -euo pipefail

bin_dir="$HOME/.local/bin"
shim_path="$bin_dir/codex"
helper_dir="$HOME/.local/lib/enterlight"
helper_path="$helper_dir/enterlight-linux-amd64"
hook_wrapper="$helper_dir/codex-hook-wsl"
config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/enterlight"
config_path="$config_dir/wsl.json"
hooks_path="$HOME/.codex/hooks.json"
marker="# Managed by Enterlight WSL approval adapter"
hook_marker="# Managed by Enterlight WSL Codex lifecycle hooks"

hooks_managed=false
if [[ -f "$config_path" ]] && grep -Eq '"codexHooksManaged"[[:space:]]*:[[:space:]]*true' "$config_path"; then
  hooks_managed=true
fi

if $hooks_managed && [[ -e "$hook_wrapper" || -L "$hook_wrapper" ]]; then
  if [[ ! -f "$hook_wrapper" ]] || ! head -n 2 -- "$hook_wrapper" | grep -Fqx "$hook_marker"; then
    echo "Refusing to remove non-Enterlight file: $hook_wrapper" >&2
    exit 1
  fi
fi

if [[ -e "$shim_path" || -L "$shim_path" ]]; then
  if [[ ! -f "$shim_path" ]] || ! head -n 2 -- "$shim_path" | grep -Fqx "$marker"; then
    echo "Refusing to remove non-Enterlight file: $shim_path" >&2
    exit 1
  fi
fi

if $hooks_managed; then
  if [[ -f "$hooks_path" ]]; then
    if [[ ! -x "$helper_path" ]]; then
      echo "Cannot remove managed Codex hooks because the Enterlight WSL helper is missing: $helper_path" >&2
      exit 1
    fi
    ENTERLIGHT_WSL_INTERNAL_MANAGE_HOOKS=1 "$helper_path" uninstall-codex-hooks "$hooks_path"
  fi
  rm -f -- "$hook_wrapper"
fi

if [[ -e "$shim_path" || -L "$shim_path" ]]; then
  rm -f -- "$shim_path"
fi

rm -f -- "$helper_path" "$config_path"
rmdir -- "$helper_dir" "$config_dir" 2>/dev/null || true
echo "Removed the Enterlight WSL adapter. Shell and Codex settings were not changed."
