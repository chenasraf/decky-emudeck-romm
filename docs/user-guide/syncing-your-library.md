# Syncing Your Library

Syncing fetches your RomM game library and creates Non-Steam shortcuts in Steam for every game. After syncing, your games appear in the Steam Library with cover art, metadata, and organized into collections.

## Where ROMs Land

The plugin downloads ROMs into your EmuDeck `roms/` tree — the canonical EmuDeck layout that ES-DE and your installed emulators expect to find ROMs in:

- **Internal SSD**: `~/Emulation/roms/<system>/<filename>`
- **SD card**: `/run/media/deck/<sd-label>/Emulation/roms/<system>/<filename>`

The plugin reads `~/.config/EmuDeck/settings.sh` to discover where your `emulationPath` lives, so SD-card installs (where EmuDeck rewrites the path to a `/run/media/...` mount) are picked up automatically.

The `<system>` folder name is translated from the RomM platform slug via `defaults/platform_map_emudeck.json`. Most slugs pass through unchanged (`snes`, `n64`, `gba`); a few rename: RomM's `ps` becomes EmuDeck's `psx`, `3ds` becomes `n3ds`, and the `mame*` / `fbneo` / `cps*` family defaults to `arcade`. See [EmuDeck Filesystem Layout — ROMs](../architecture/emudeck-layout.md#roms) for the full mapping.

## Manual by default, Automatic if you opt in

This fork is a browse-first client: by default, **no platform auto-syncs**. The Library tab lets you pick individual ROMs and download them on demand. If you want a whole platform's library to download in the background — the upstream "sync everything" behavior — flip that platform's **Sync mode** to `Automatic` on the Platforms page.

- `Manual` (default): the platform appears in the Library tab for browsing and on-demand download; **Sync Library** ignores it.
- `Automatic`: **Sync Library** downloads the platform's full library in one batch, the same way upstream `decky-romm-sync` does.

You can mix modes: some platforms on `Automatic` for the "just have everything" path, others on `Manual` for "only what I tap."

## How Sync Works (Automatic platforms)

1. The plugin fetches all ROMs from your RomM server (filtered to platforms with sync mode `Automatic`)
2. For each ROM, a Non-Steam shortcut is created in Steam via the SteamClient API — no restart required
3. Cover art from RomM is applied as the portrait grid image
4. If you have a SteamGridDB API key configured, hero banners, logos, and wide grid images are also fetched
5. Metadata (description, developer, genres, release date) is cached and displayed in the plugin's custom game detail panel
6. Steam collections are created per platform (e.g. "RomM: Game Boy Advance (steamdeck)")

## Starting an Automatic Sync

1. Open the QAM and navigate to the plugin
2. Tap **Sync Library** on the main page
3. A progress bar shows the sync status
4. When complete, a summary appears (e.g. "Added 42 games from 5 platforms")

If every platform is set to `Manual` (the default), **Sync Library** finishes immediately with nothing to do. That's expected — head to the Library tab and tap **Download** on the ROMs you want.

![RomM Sync QAM panel with connection status and the Sync Library button](../assets/screenshot-qam.jpg)

<!-- Screenshot: Sync in progress with progress bar -->

You can tap **Cancel Sync** to stop mid-sync. Games already added will remain.

## Per-Platform Toggles

Use the **Platforms** page to enable platforms and pick a sync mode per platform.

1. From the main page, tap **Platforms**
2. Each platform shows its name and ROM count
3. Toggle platforms on or off (disabled platforms don't appear in the Library tab either)
4. For enabled platforms, choose **Sync mode**: `Manual` (browse + on-demand) or `Automatic` (full-library download on Sync)
5. Use **Enable All** / **Disable All** for bulk changes

<!-- Screenshot: Platforms page with toggle switches and ROM counts -->

## Collections

The plugin automatically creates Steam collections for each synced platform. Collection names include your machine's hostname to avoid conflicts if you run the plugin on multiple devices:

- `RomM: Nintendo 64 (steamdeck)`
- `RomM: Game Boy Advance (steamdeck)`
- `RomM: PlayStation (htpc)`

Collections appear in Steam's library sidebar and can be used to browse games by platform.

## Artwork

Each synced game gets up to five types of artwork:

| Type | Source | Where It Appears |
| --- | --- | --- |
| Portrait Grid (cover) | RomM | Library grid tiles, collections |
| Hero Banner | SteamGridDB | Game detail page background |
| Logo | SteamGridDB | Title overlay on hero banner |
| Wide Grid | SteamGridDB | Recent games shelf, list view |
| Icon | SteamGridDB | Taskbar, small UI elements |

Cover art is always applied from RomM. The other four types require a [SteamGridDB API key](configuration.md#steamgriddb-api-key). Games without a SteamGridDB match will show Steam's default placeholders for those slots.

You can refresh artwork for any individual game from its [game detail page](managing-games.md#refreshing-artwork-and-metadata).

## Re-Syncing

Running sync again updates your library with any changes from RomM (new ROMs, removed platforms, etc.). Existing shortcuts are updated rather than duplicated.

## Removing Shortcuts

To remove synced games, use the **Danger Zone** page. See [Troubleshooting — Danger Zone](troubleshooting.md#danger-zone) for details on the available removal options.

---

**Previous:** [Configuration](configuration.md) | **Next:** [Managing Games](managing-games.md)
