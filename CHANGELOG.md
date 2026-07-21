# Changelog

## Unreleased

## v0.2.1 — 2026-07-22

- Add opt-in WSL Codex lifecycle hooks that return successfully when Windows notifications fail or time out.
- Preserve unrelated hooks and remove only installer-managed Enterlight handlers during uninstall.

## v0.2.0 — 2026-07-20

- Animate full-keyboard working, waiting, completed, error, and visibility-aware idle scenes.
- Compose the four most recently active sessions as horizontal lanes.
- Add persistent calm, vivid, and max presentation presets.
- Add fail-closed approval candidate lighting with a short renewable lease.
- Preserve Codex session IDs from lifecycle hook payloads.
- Add a WSL2 PTY adapter for Codex CLI 0.144.6 approval candidates in the VS Code integrated terminal.
- Add managed WSL install/uninstall scripts and a Linux amd64 release artifact without changing shell or Codex settings.
- Fail closed when concurrent adapter sessions, unknown prompt text, or incomplete terminal snapshots make approval lighting ambiguous.

## v0.1.0 — 2026-07-20

- Light only the Enter key through the official Razer Chroma REST API.
- Built-in states: working, waiting, done, error and off.
- Auto-starting local daemon with Chroma heartbeat management.
- Codex lifecycle hook installer and uninstaller.
- Windows amd64 and arm64 release workflow.
