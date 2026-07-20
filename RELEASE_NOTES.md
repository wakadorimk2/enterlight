# Enterlight v0.1.0

Your Razer keyboard's Enter key is now a status light for Codex.

- **Blue:** working
- **Amber breathing:** waiting for approval or input
- **Green pulse:** done
- **Red blink:** error
- **Off:** releases Chroma so Synapse restores your usual profile

Run `enterlight install-codex`, review the generated lifecycle hooks with `/hooks`, and you are ready.

This first release targets Windows and Razer Chroma keyboards. It was designed around the DeathStalker family but uses Razer's generic `RZKEY_ENTER` mapping.
