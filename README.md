# Enterlight

Turn the **Enter key** on a Razer Chroma keyboard into a tiny status beacon for Codex and other coding agents.

```text
working  → blue, solid
waiting  → amber, breathing
done     → green, three pulses, then restore Synapse
error    → red, blinking
off      → release Chroma and restore the normal Synapse profile
```

The rest of the keyboard is dark while an Enterlight state is active. `off` and the end of `done` release the Chroma session so Razer Synapse can restore your usual lighting profile.

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

To remove only Enterlight's handlers:

```powershell
enterlight uninstall-codex
```

## Manual CLI

```powershell
enterlight working
enterlight waiting
enterlight done
enterlight error
enterlight off
enterlight status
enterlight stop-daemon
```

## How it works

Enterlight is a dependency-free Go executable. A tiny local daemon listens only on `127.0.0.1:47812`, maintains the Razer Chroma REST session and heartbeat, and renders a `CHROMA_CUSTOM_KEY` effect at `RZKEY_ENTER` (`0x030E`). CLI calls and Codex hooks only send a small local state update, so they return immediately.

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

- Windows and Razer Chroma only in v0.1.
- Chroma custom effects take over the keyboard while active; Enterlight intentionally renders every key except Enter as black.
- Codex hooks are experimental and require explicit trust in `/hooks`.
- Hardware behavior can differ by keyboard firmware and Synapse version. Please report the exact model and layout with bugs.

## Development

```bash
go test ./...
go build ./cmd/enterlight
```

Build a Windows binary from any Go host:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build \
  -ldflags "-s -w -X main.version=v0.1.0" \
  -o dist/enterlight-windows-amd64.exe ./cmd/enterlight
```

## License

MIT
