![EmuDeck RomM Sync](assets/banner.png)

[![CI](https://github.com/chenasraf/decky-emudeck-romm/actions/workflows/ci.yml/badge.svg)](https://github.com/chenasraf/decky-emudeck-romm/actions/workflows/ci.yml)
[![Downloads](https://img.shields.io/github/downloads/chenasraf/decky-emudeck-romm/total.svg)](https://github.com/chenasraf/decky-emudeck-romm/releases)

# EmuDeck RomM Sync

> **EmuDeck-focused fork** of [danielcopper/decky-romm-sync](https://github.com/danielcopper/decky-romm-sync).
> The product thesis differs from upstream — see [Roadmap](#roadmap) below. Today the plugin still ships
> identical to upstream's behaviour; the EmuDeck-specific changes land in phases.

A [Decky Loader](https://decky.xyz/) plugin that syncs your self-hosted [RomM](https://github.com/rommapp/romm) library
into your Steam Deck. Browse and download ROMs from the QAM, manage BIOS files, keep saves in sync, and (currently)
launch through [RetroDECK](https://retrodeck.net/).

## 📖 [Read the full documentation →](https://chenasraf.github.io/decky-emudeck-romm/)

Installation, setup, save sync, BIOS management, troubleshooting, and the architecture reference all live on the
documentation site. This README is just the quick tour.

## Roadmap

This fork is being reshaped around an EmuDeck-first workflow:

- **EmuDeck-native paths** for ROMs, BIOS, and per-emulator saves
- **File-manager-first** browse + on-demand download from RomM
- **Bidirectional save sync** with per-system filename normalization
- **Steam shortcuts** demoted to opt-in / secondary
- **RetroDECK** still supported as a secondary launcher

## Features

- **Library sync** — Pulls platforms and ROMs from your RomM server and creates Steam shortcuts, complete with cover
  art, hero banners, and logos (with optional [SteamGridDB](https://www.steamgriddb.com/) artwork)
- **Save sync** — Keeps save files in sync across devices through your RomM server, with newest-wins conflict
  resolution and a manual override when you need it
- **ROM downloads** — Download ROMs on demand with progress tracking and a managed download queue
- **BIOS management** — Download firmware/BIOS files from RomM for systems that need them (PSX, Dreamcast, PS2, …)
- **Game detail page** — Install status, BIOS status, and download/uninstall actions right on each game's Steam page
- **Per-platform control** — Choose exactly which platforms get synced
- **Controller friendly** — Full gamepad navigation throughout the plugin UI
- **Steam Input config** — Per-shortcut Steam Input mode (Default / Force On / Force Off)
- **RetroDECK path migration** — Move your RetroDECK installation between storage locations without re-syncing
- **RetroArch diagnostics** — Detects misconfigured input drivers that break menu navigation

## Screenshots

> Screenshots below are inherited from upstream — replacement pending a real-device run of this fork.

| QAM panel | Game detail page |
| :---: | :---: |
| ![RomM Sync QAM panel](assets/screenshot-qam.jpg) | ![Game detail page with metadata](assets/screenshot-game-detail.jpg) |
| **BIOS management** | **Per-game actions** |
| ![BIOS status showing a required file to download](assets/screenshot-bios.jpg) | ![RomM actions menu: sync saves, download BIOS, refresh artwork](assets/screenshot-actions.jpg) |

## Requirements

- [Decky Loader](https://decky.xyz/) on your Steam Deck or Linux HTPC
- A running [RomM](https://github.com/rommapp/romm) server, **version 4.8.1 or newer** (the plugin stays inert against
  older servers)
- A launcher for your ROMs — [RetroDECK](https://retrodeck.net/) today;
  [EmuDeck](https://www.emudeck.com/) support is the target of the in-progress rewrite

## Installation

### From the Decky Store

> ⚠️ **Not available yet.** This fork is not on the Decky Store. Use the manual install below.

### From ZIP or URL

Requires **Developer Mode** in Decky Loader (Decky settings → gear icon → toggle **Developer Mode**).

1. Download the latest `decky-emudeck-romm.zip` from the
   [releases page](https://github.com/chenasraf/decky-emudeck-romm/releases)
2. In Decky settings → **Developer** tab → **Install Plugin from ZIP** (or **from URL** with the
   [latest release link](https://github.com/chenasraf/decky-emudeck-romm/releases/latest/download/decky-emudeck-romm.zip))

> Full step-by-step instructions, including first-time setup, are in
> [Getting Started](https://chenasraf.github.io/decky-emudeck-romm/user-guide/getting-started/).

## Quick start

1. Open the Quick Access Menu and select **EmuDeck RomM Sync**
2. In **Settings**, enter your RomM server URL and credentials, then hit **Test Connection**
3. In **Platforms**, enable the platforms you want to sync
4. Hit **Sync Library** — your ROMs appear as non-steam shortcuts

See the [User Guide](https://chenasraf.github.io/decky-emudeck-romm/user-guide/syncing-your-library/) for syncing
details, [save sync](https://chenasraf.github.io/decky-emudeck-romm/user-guide/save-sync/), and
[BIOS management](https://chenasraf.github.io/decky-emudeck-romm/user-guide/bios-management/).

## Contributing

Build from source, run the tests, and read the architecture reference on the documentation site:

- [Development setup](https://chenasraf.github.io/decky-emudeck-romm/contributing/development/)
- [Backend architecture](https://chenasraf.github.io/decky-emudeck-romm/architecture/backend-architecture/)

## Acknowledgments

This plugin stands on the shoulders of some great projects:

- [danielcopper/decky-romm-sync](https://github.com/danielcopper/decky-romm-sync) — the upstream project this fork
  derives from. Every line of working code in this repo started life there
- [RomM](https://github.com/rommapp/romm) — the self-hosted ROM manager at the heart of this plugin. RomM provides
  the library, metadata, cover art, and save file storage that makes the entire sync experience possible
- [RetroDECK](https://retrodeck.net/) — the all-in-one emulation solution for Steam Deck that bundles ES-DE,
  RetroArch, and standalone emulators into a single flatpak
- [EmuDeck](https://www.emudeck.com/) — the emulation setup this fork is being reshaped around
- [Decky Loader](https://decky.xyz/) — the plugin framework that makes all of this possible
- [Valve](https://www.valvesoftware.com/) — for the Steam Deck, SteamOS, and an open enough platform to build on
- [Unifideck](https://github.com/ma3ke/unifideck) — inspiration for game detail page injection techniques and gamepad
  navigation patterns
- [MetaDeck](https://github.com/EmuDeck/MetaDeck) — inspiration for store patching patterns used in metadata display
  on non-Steam shortcuts

## License

GPL-3.0 — see [LICENSE](LICENSE). The original copyright belongs to the upstream authors of
[danielcopper/decky-romm-sync](https://github.com/danielcopper/decky-romm-sync); fork-specific changes are
copyright Chen Asraf.
