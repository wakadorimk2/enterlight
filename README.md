# Enterlight

Turn a Razer Chroma keyboard into an animated status surface for Codex and other coding agents.

```text
working  → cyan/purple wave
waiting  → amber breathing
done     → green/white completion burst
error    → red/orange warning
idle     → dim ambient drift while its panel is visible
off      → release Chroma and restore the normal Synapse profile
```

Up to four active sessions are composed as horizontal lanes. Active scenes remain lit across application switches; idle scenes require an explicit panel-visibility signal. `off`, or the end of the last `done` scene, releases the Chroma session so Razer Synapse can restore your usual lighting profile.

## Requirements

- Windows 10/11
- A Razer Chroma keyboard with per-key lighting (developed for the DeathStalker family)
- Razer Synapse with Chroma Connect / **Razer Chroma SDK Service** running

## Install

Download `enterlight-windows-amd64.exe` from the latest GitHub release, rename it to `enterlight.exe`, and put it somewhere on your `PATH`.

Then run:

```powershell
enterlight doctor
enterlight waiting
enterlight done
enterlight preset calm
```

The background daemon starts automatically on the first state command.

## Codex hooks

```powershell
enterlight install-codex
```

This safely appends Enterlight handlers to `%USERPROFILE%\.codex\hooks.json` and writes a `.bak` backup when the file already exists.

Restart Codex, open `/hooks`, review the new hooks, and trust them. Enterlight maps:

- `UserPromptSubmit` and `PreToolUse` → `working`
- `PermissionRequest` → `waiting`
- `Stop` → `done`

The hook payload's Codex `session_id` keeps concurrent sessions in separate lanes. The `PermissionRequest` hook marks a session as waiting, but it does not guess which approval choices are visible. Decision-specific key lighting is enabled only through the explicit adapter API below.

To remove only Enterlight's handlers:

```powershell
enterlight uninstall-codex
```

## WSL2 Codex CLI approval adapter

The WSL adapter supports **Codex CLI 0.144.6 running in WSL2 inside the VS Code integrated terminal**. It keeps the normal launch command (`codex [args...]`) while reflecting numbered approval choices on the matching number keys and the currently selected choice on Enter.

Install the Windows release first and make `enterlight.exe` available to WSL. Then download `enterlight-linux-amd64` plus `scripts/install-wsl.sh` from this repository or the release, and run in WSL2:

```bash
chmod +x enterlight-linux-amd64 install-wsl.sh
./install-wsl.sh \
  --helper "$PWD/enterlight-linux-amd64" \
  --windows-exe /mnt/c/path/to/enterlight.exe
codex --version
```

The installer resolves and records the existing Codex executable before installing a managed `~/.local/bin/codex` shim. It stops without changing anything if `~/.local/bin` is not already on `PATH`, Codex is not exactly version 0.144.6, the shim path contains a non-Enterlight file, either executable cannot be resolved, or the paths would recurse. By default, it does not edit shell startup files or Codex settings.

Codex lifecycle hooks are optional. To install them for both WSL and Windows when the same `hooks.json` is shared, add the explicit opt-in flag:

```bash
./install-wsl.sh \
  --helper "$PWD/enterlight-linux-amd64" \
  --windows-exe /mnt/c/path/to/enterlight.exe \
  --install-codex-hooks
```

The opt-in safely merges only Enterlight handlers into `~/.codex/hooks.json`, preserves unrelated hooks, and backs up an existing file as `hooks.json.bak`. WSL hooks call a managed wrapper while `commandWindows` continues to call the Windows Enterlight executable directly. Notifications are best effort: the wrapper accepts only the five lifecycle states, stays silent if Windows interop or Enterlight fails, and uses a three-second process timeout so it returns before Codex's five-second hook timeout. Re-running the installer is safe; installing without the flag leaves hook files and hook ownership unchanged.

To uninstall the WSL adapter and any lifecycle hooks that this installer recorded as managed:

```bash
./uninstall-wsl.sh
```

The adapter forwards the terminal PTY, arguments, environment, working directory, terminal resize, signals, and exit status. Raw input is forwarded immediately and is never parsed or logged. Output tracking retains only rows that can still match the approval title and fixed 0.144.6 candidate allowlist; it does not save terminal history, commands, conversations, or typed text.

The overlay fails closed (clears) for unknown UI text or Codex versions, dynamic command-prefix choices, duplicate or out-of-range numbers, missing or multiple selections, multiple simultaneous prompts, Windows bridge failure, lease expiry, or overlap with a different wrapper session. A visible valid prompt renews the existing five-second lease; prompt dismissal, cancellation, and CLI exit clear it.

## Manual CLI

```powershell
enterlight working
enterlight waiting
enterlight done
enterlight error
enterlight idle
enterlight off
enterlight preset vivid
enterlight session my-agent working
enterlight visible my-agent true
enterlight status
enterlight stop-daemon
```

Presentation presets preserve the same state meanings while changing brightness and speed:

- `calm` (default): 35% brightness, slow motion
- `vivid`: 70% brightness, medium motion
- `max`: 100% brightness, fast motion

The selected preset is stored in the user's Enterlight config directory and survives daemon restarts.

## Approval adapter API

Surface-specific adapters can provide a verified candidate list and current selection. Enterlight maps candidates to number keys and previews the selected decision on Enter:

```powershell
enterlight approval set `
  --session my-agent `
  --selected 2 `
  --candidate 1=allow_once `
  --candidate 2=allow_session `
  --candidate 3=decline `
  --candidate 4=cancel
```

The decision colors are blue for `allow_once`, green for `allow_session`, orange for `decline`, and red for `cancel`. Clear the overlay when the prompt closes:

```powershell
enterlight approval clear --session my-agent
```

The same operations are available to local adapters at `127.0.0.1:47812` through `POST /sessions/update`, `POST /sessions/visibility`, `POST /approval`, `DELETE /approval`, and `POST /preset`. Approval overlays expire after five seconds unless refreshed; `leaseMs` may be set from 500 through 10000 milliseconds. Invalid candidates, duplicate or out-of-range keys, unknown decisions, and invalid selections clear the matching overlay instead of guessing.

This API accepts only session metadata, decision enums, number-key positions, and visibility. Enterlight does not observe, collect, store, block, remap, or delay keyboard input.

## How it works

Enterlight is a dependency-free Go executable. A tiny local daemon listens only on `127.0.0.1:47812`, maintains the Razer Chroma REST session and heartbeat, and renders `CHROMA_CUSTOM_KEY` frames on Razer's generic 6-row × 22-column grid. CLI calls and Codex hooks only send small local state updates, so they return immediately.

Razer's REST session is released for `off`, rather than leaving a `CHROMA_NONE` effect active, so Synapse can take control again.

## Troubleshooting

Run `enterlight doctor` to see the daemon's last Chroma error and a recovery suggestion.

If commands succeed and `status` reports `"chromaConnected": true` but the keyboard stays dark, check the keyboard's brightness in Synapse. A zero-brightness or battery-saving setting can suppress Chroma SDK effects even though the SDK reports success. Wireless keyboards may behave differently while charging, on USB, or on battery power.

If it reports `chroma_client_limit`, let old Chroma REST sessions expire before restarting any Razer software:

```powershell
enterlight stop-daemon
Start-Sleep -Seconds 15
enterlight working
```

If the client limit remains after waiting, restart Razer Synapse or the **Razer Chroma SDK Service** once, then run `enterlight working` again. Enterlight never stops or restarts Razer services automatically.

## Limitations

- Razer Chroma rendering still runs on Windows; the Linux helper reaches that Windows process through WSL interop.
- Chroma custom effects take over the keyboard while active.
- Codex hooks are experimental and require explicit trust in `/hooks`.
- Approval discovery supports only Codex CLI 0.144.6 in WSL2's VS Code integrated terminal. Codex App, IDE extension WebViews, ordinary Windows Terminal sessions, and idle-panel visibility detection are outside this adapter's scope.
- Unsupported or ambiguous approval prompts remain on the generic waiting scene.
- Hardware behavior can differ by keyboard firmware and Synapse version. Please report the exact model and layout with bugs.

## Development

```bash
go test ./...
go build ./cmd/enterlight
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build \
  -o dist/enterlight-linux-amd64 ./cmd/enterlight-wsl
```

Build a Windows binary from any Go host:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags "-s -w -X main.version=v0.1.0" \
  -o dist/enterlight-windows-amd64.exe ./cmd/enterlight
```

## License

MIT
