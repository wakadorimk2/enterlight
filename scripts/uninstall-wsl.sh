#!/usr/bin/env bash
set -euo pipefail

bin_dir="$HOME/.local/bin"
shim_path="$bin_dir/codex"
helper_dir="$HOME/.local/lib/enterlight"
helper_path="$helper_dir/enterlight-linux-amd64"
config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/enterlight"
config_path="$config_dir/wsl.json"
marker="# Managed by Enterlight WSL approval adapter"

if [[ -e "$shim_path" || -L "$shim_path" ]]; then
  if [[ ! -f "$shim_path" ]] || ! head -n 2 -- "$shim_path" | grep -Fqx "$marker"; then
    echo "Refusing to remove non-Enterlight file: $shim_path" >&2
    exit 1
  fi
  rm -f -- "$shim_path"
fi

rm -f -- "$helper_path" "$config_path"
rmdir -- "$helper_dir" "$config_dir" 2>/dev/null || true
echo "Removed the Enterlight WSL adapter. Shell and Codex settings were not changed."
