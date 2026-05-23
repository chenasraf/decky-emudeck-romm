# Getting Started

## What is EmuDeck RomM Sync?

EmuDeck RomM Sync is a [Decky Loader](https://decky.xyz/) plugin that connects your self-hosted
[RomM](https://github.com/rommapp/romm) ROM library to your Steam Deck. It is an **EmuDeck-focused fork** of
[danielcopper/decky-romm-sync](https://github.com/danielcopper/decky-romm-sync) — today the plugin still ships
identical to upstream's behaviour (Non-Steam shortcuts launching through RetroDECK); the EmuDeck-native flow is being
introduced in phases. Every game in your RomM library appears as a Non-Steam shortcut in the Steam Library, complete
with cover art, metadata, and collections.

## Prerequisites

Before installing the plugin, you need:

1. **A RomM server** — a running RomM instance with your ROM library. You'll need the server URL, a username, and a password. Each user should have their own RomM account (see [Save Sync](save-sync.md) for why this matters).

2. **An emulation frontend** — [RetroDECK](https://retrodeck.net/) today (the current default the plugin launches
   ROMs through). [EmuDeck](https://www.emudeck.com/) support is the target of the in-progress rewrite; once
   it lands, EmuDeck will become the recommended setup and RetroDECK will remain an opt-in alternative.

3. **Decky Loader** — the plugin framework for Steam's Gaming Mode. Install it from [decky.xyz](https://decky.xyz/) if you haven't already.

4. **A personal RomM account** — save sync ties saves to the authenticated user. Use your own account, not a shared one.

## Installation

### From Decky's "Install Plugin From URL"

1. Open the Quick Access Menu (QAM) in Gaming Mode by pressing the **...** button
2. Go to the Decky Loader tab (the plug icon) and open settings (gear icon)
3. Under **General → Other**, enable **Developer Mode** — a new **Developer** tab appears in the sidebar
4. Open the **Developer** tab and select **Install Plugin From URL**
5. Enter the direct URL to the release zip

   The URL for the latest release follows this pattern:

   ```text
   https://github.com/chenasraf/decky-emudeck-romm/releases/download/decky-emudeck-romm-v{VERSION}/decky-emudeck-romm.zip
   ```

   For example, for v0.1.0:

   ```text
   https://github.com/chenasraf/decky-emudeck-romm/releases/download/decky-emudeck-romm-v0.1.0/decky-emudeck-romm.zip
   ```

6. Decky downloads and installs the plugin automatically — no restart needed

**Tip:** You can also open the [releases page](https://github.com/chenasraf/decky-emudeck-romm/releases) in Steam's built-in browser (Gaming Mode → long-press the Steam button → Web Browser), long-press the zip download link, and copy the URL from there.

Any direct URL to the zip file works (GitHub releases, a self-hosted mirror, etc.) as long as it points to a valid `.zip` containing the plugin.

### Manual installation (alternative)

1. Download `decky-emudeck-romm.zip` from the [releases page](https://github.com/chenasraf/decky-emudeck-romm/releases)
2. Extract the zip to `~/homebrew/plugins/` on your device (via SSH, file manager, or USB)
3. Restart Decky Loader — either reboot, or run `sudo systemctl restart plugin_loader` via SSH
4. The plugin appears in your QAM under the Decky tab

## First-Time Setup

After installation, you need to connect the plugin to your RomM server:

1. Open the QAM and find **EmuDeck RomM Sync**
2. Tap **Connection Settings**
3. Enter your RomM server URL (e.g. `http://192.168.1.100:8080`)
4. Enter your username and password
5. Tap **Save Settings**, then **Test Connection** to verify

<!-- Screenshot: Connection Settings page with URL, username, and password fields -->

Once connected, you're ready to sync your library. See [Configuration](configuration.md) for additional settings, or jump straight to [Syncing Your Library](syncing-your-library.md).

---

**Next:** [Configuration](configuration.md)
