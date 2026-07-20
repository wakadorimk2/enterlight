# Next release

Enterlight can now use the whole keyboard as an animated Codex status surface. Concurrent sessions occupy up to four horizontal lanes, and calm, vivid, and max presets vary the presentation without changing state meanings.

An explicit local adapter API can light verified approval candidates on number keys and preview the current selection on Enter. The overlay fails closed and expires unless refreshed; Enterlight itself never observes keyboard input. Codex App, CLI, and IDE candidate-discovery adapters remain future work.

# Enterlight v0.1.0

Your Razer keyboard's Enter key is now a status light for Codex.

- **Blue:** working
- **Amber breathing:** waiting for approval or input
- **Green pulse:** done
- **Red blink:** error
- **Off:** releases Chroma so Synapse restores your usual profile

Run `enterlight install-codex`, review the generated lifecycle hooks with `/hooks`, and you are ready.

This first release targets Windows and Razer Chroma keyboards. It was designed around the DeathStalker family but uses Razer's generic `RZKEY_ENTER` mapping.
