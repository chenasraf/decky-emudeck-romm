# EmuDeck filesystem layout

How EmuDeck arranges ROMs, BIOS, saves, configuration, and Steam-ROM-Manager launchers on disk. The `EmuDeckFrontendAdapter` (`adapters/frontends/emudeck.py`) uses this page as its source of truth.

!!! info "Empirical snapshot"
    Last verified **2026-05-24** against a Steam Deck (LCD) running **SteamOS 3.7.25**. Items marked **observed** were confirmed on that install; items marked **canonical** come from `~/.config/EmuDeck/backend/settings.sh` and apply to every EmuDeck install. Items marked **TODO** need a second sample to confirm. Example paths in this page use the default internal-SSD layout (`~/Emulation`); SD-card installs put the same tree under `/run/media/deck/<sd-label>/Emulation` — see [Canonical roots](#canonical-roots-read-these-do-not-guess) below.

## Canonical roots: read these, do not guess

EmuDeck writes one shell script that every other path is derived from:

```bash
# ~/.config/EmuDeck/settings.sh   (default internal-SSD install)
emulationPath="/home/deck/Emulation"
romsPath="/home/deck/Emulation/roms"
toolsPath="…"
```

On SD-card installs EmuDeck concatenates the SD mount with `/Emulation`, producing literal quoting like `emulationPath="/run/media/deck/512GB"/Emulation`. **The adapter MUST parse this file rather than assume `~/Emulation`** — the user's chosen disk decides everything downstream, and a fresh re-run of EmuDeck's setup can move the tree to a new disk by rewriting `settings.sh`.

The convention this page uses below:

- `$emulationPath` → the root from `settings.sh` (`~/Emulation` by default; `/run/media/deck/<label>/Emulation` on SD-card installs)
- `$romsPath` → `$emulationPath/roms`
- `$saves` → `$emulationPath/saves` (EmuDeck's centralized save tree — see [Saves](#saves-the-central-tree-and-the-flatpak-exceptions))

## EmuDeck version surface

There is **no single `EmuDeck version` string on disk**. The closest things:

| Path | What it tells you |
| --- | --- |
| `~/.config/EmuDeck/backend/.git/HEAD` | The git ref of the backend repo EmuDeck cloned. **canonical** — every install has it. |
| `~/.config/EmuDeck/backend/versions.json` | Per-emulator schema-version integers (`{"ra": {"version": 2}, "dolphin": {"version": 4}, …}`). EmuDeck increments these when an emulator's setup logic changes. **canonical**. |
| `~/.config/EmuDeck/backend/branch.txt` | 5-byte file naming the branch (e.g. `main`). **observed**. |
| `~/.config/EmuDeck/.updaterId` | 36-byte UUID identifying the install. Not a version. |

**Recommendation for B11 (version probe)**: read the backend git ref (`HEAD` + the resolved commit), and treat the per-emulator `versions.json` integers as a secondary compatibility signal. The plugin's `_MIN_TESTED_VERSION` band should track the *backend git short-SHA* observed on the dev Deck, not a "EmuDeck v1.2.3" string that doesn't exist. The observed checkout on 2026-05-24 was last touched **2026-03-24** (`mtime` of `~/.config/EmuDeck/backend/`). The `versions.json` schema integers observed: `ra=2 dolphin=4 pcsx2=1 yuzu=1 srm=9 esde=5 pegasus=2 ryujinx=4 xenia=2 flycast=1 model2=1 supermodel=1` (everything else `0`).

## ROMs

User-facing reference: [Syncing Your Library — Where ROMs Land](../user-guide/syncing-your-library.md#where-roms-land).

`$romsPath` is a flat list of per-system directories. Observed on this install (~150 system slugs — only the meaningful ones listed; the long tail is in `inventory.out`):

| EmuDeck slug | Console | RomM equivalent (best guess) |
| --- | --- | --- |
| `psx` | PlayStation | `ps` |
| `ps2` | PlayStation 2 | `ps2` |
| `psp` | PSP | `psp` |
| `psvita` | PS Vita | `psvita` |
| `ps3` | PS3 | `ps3` |
| `ps4` | PS4 | `ps4` |
| `snes`, `sfc` | SNES (region split) | `snes` |
| `nes`, `famicom`, `fds` | NES family | `nes` / `famicom` / `fds` |
| `gb`, `gbc`, `gba`, `gba_old` | Game Boy line | `gb` / `gbc` / `gba` |
| `nds`, `n3ds` | DS / 3DS | `nds` / `n3ds` |
| `n64`, `n64dd` | N64 | `n64` |
| `gc`, `wii`, `wiiu`, `switch` | Nintendo home consoles | `gc` / `wii` / `wiiu` / `switch` |
| `genesis`, `genesiswide`, `megadrive`, `megadrivejp` | Mega Drive / Genesis | `genesis` |
| `sega32x`, `sega32xna`, `sega32xjp` | 32X | `sega32x` |
| `segacd`, `megacd`, `megacdjp` | Sega CD | `segacd` |
| `saturn`, `saturnjp` | Saturn | `saturn` |
| `dreamcast`, `naomi`, `naomi2`, `naomigd`, `atomiswave` | Dreamcast / NAOMI | `dreamcast` |
| `xbox`, `xbox360` | Xbox / Xbox 360 | `xbox` / `xbox360` |
| `arcade`, `mame`, `mame-advmame`, `mame-mame4all`, `fbneo`, `fba`, `cps`, `cps1`, `cps2`, `cps3`, `neogeo` | Arcade | mostly `arcade` |
| `dos`, `pc`, `pc88`, `pc98`, `x68000`, `fmtowns` | DOS / vintage PC | `dos` etc. |
| `scummvm` | ScummVM | `scummvm` |
| `c64`, `c16`, `amiga`, `amiga600`, `amiga1200`, `amigacd32`, `cdtv`, `vic20`, `atarist`, `zxspectrum`, `bbcmicro`, `msx`, `msx1`, `msx2`, `msxturbor` | 8/16-bit micros | per-platform |
| `atari2600`, `atari5200`, `atari7800`, `atari800`, `atarilynx`, `atarijaguar`, `atarijaguarcd`, `atarixe` | Atari line | per-platform |
| `3do`, `coleco`, `vectrex`, `intellivision`, `crvision`, `pv1000`, `videopac`, `odyssey2`, `gamegear`, `mastersystem`, `sg-1000` | Misc retro | per-platform |
| `pcengine`, `pcenginecd`, `tg16`, `tg-cd`, `supergrafx`, `pcfx` | PC Engine family | per-platform |
| `daphne`, `model2`, `model3`, `naomi*`, `supermodel` | Arcade specialty | per-platform |
| `ports`, `cavestory`, `doom`, `quake`, `lutro`, `easyrpg`, `solarus`, `tic80`, `pico8`, `chailove`, `mugen`, `openbor`, `stratagus`, `wasm4` | Engines / homebrew | per-engine |
| `steam`, `epic`, `lutris`, `cloud`, `moonlight`, `remoteplay`, `desktop`, `generic-applications` | Non-emulator launchers | not RomM-syncable |

A platform-slug-mapping table (`defaults/platform_map_emudeck.json`) lands in Phase 2 and will need to translate `(romm_slug, console_id) → emudeck_slug`. Notable mismatches vs. RetroDECK's mapping:

- EmuDeck splits regional variants of the same console into separate slugs (`snes` / `sfc`, `genesis` / `megadrive` / `megadrivejp`, `saturn` / `saturnjp`, `megacd` / `megacdjp` / `segacd`). RetroDECK does not — decide whether to mirror EmuDeck's split (drop ROMs into the matching region folder) or coerce everything into the non-`jp` slug. Recommendation: mirror the split when RomM provides region metadata, fall back to the non-`jp` slug otherwise.
- EmuDeck has both `mame`, `mame-advmame`, `mame-mame4all`, and `arcade` — these are different launcher targets, not the same ROM. The chosen slug determines which emulator launches the file. Recommendation: default to `arcade` for unknown arcade ROMs, surface the per-platform emulator picker (Phase 4) when the user wants control.
- EmuDeck `psx` vs. RomM `ps` — slug rename in the table.

## BIOS

User-facing reference: [BIOS Management](../user-guide/bios-management.md).

`$emulationPath/bios/` mixes bare files and per-system subdirs:

```text
$emulationPath/bios/
├── HdPacks/
├── IPL.bin                                  # GameCube IPL
├── Mupen64plus/{cache,shaders}/
├── PPSSPP/{lang,flash0,debugger,themes,…}/  # PPSSPP runtime data, not "BIOS"
├── SCPH-90001_BIOS_V18_USA_230.{DIFF,INF,MEC,NVM,ROM0,ROM1}  # PS2
├── SCPH50003.{bin,mec,nvm}                  # PS2 (older)
├── azahar/                                  # 3DS (azahar fork)
├── bios7.bin, bios9.bin                     # NDS
├── citra/, citra-flatpak/                   # 3DS — note two parallel dirs
├── dc/                                      # Dreamcast
├── firmware.bin, firmware.bin.bak           # NDS firmware
├── kronos/, lime3ds/                        # Saturn / 3DS forks
├── mame/{bios}/                             # MAME BIOS
├── neocd/                                   # NeoGeo CD
├── ryujinx/                                 # Switch keys
├── same_cdi/{bios}/                         # CD-i
├── scph5500.bin, scph5501.bin, scph5502.bin # PS1 (region variants)
├── shadps4/                                 # PS4
└── yuzu/                                    # Switch keys (yuzu)
```

The mix of bare files + per-system dirs is intentional — many BIOSes are matched by hash to a specific filename at `$emulationPath/bios/<exact-filename>.bin`, not nested. The plugin's existing `bios_registry.json` (inherited from upstream) already knows expected filenames; the destination root just becomes `$emulationPath/bios/` and the registry's per-file relative path is appended.

Subdirs like `citra-flatpak/` exist because Flatpak emulators can't read outside their sandbox, so EmuDeck mirrors some BIOSes into Flatpak-visible locations during setup. The plugin should write to the canonical `$emulationPath/bios/` location — EmuDeck's setup scripts handle the mirroring. **TODO**: confirm by writing a fresh BIOS and watching whether EmuDeck mirrors it on next launch.

## Saves: the central tree and the Flatpak exceptions

EmuDeck **mostly** centralizes saves under `$emulationPath/saves/` — one subdir per emulator, usually with `{saves, states}` underneath. Observed:

```text
$emulationPath/saves/
├── BigPEmu/
├── Cemu/
├── MAME/{saves,states}/
├── RMG/{saves,states}/
├── Vita3K/
├── azahar/        # 3DS (azahar)
├── citra/         # 3DS (legacy Citra)
├── dolphin/       # GC + Wii
├── duckstation/{saves,states}/    # PS1
├── es-de/         # ES-DE itself
├── lime3ds/       # 3DS (lime3ds fork)
├── melonds/{saves,states}/         # NDS
├── mgba/{saves,states}/            # GBA
├── pcsx2/{saves,states}/           # PS2
├── ppsspp/        # PSP
├── primehack/     # Wii (Metroid Prime)
├── retroarch/     # RetroArch — **see exception below**
├── rpcs3/         # PS3
├── ryujinx/       # Switch
├── scummvm/saves/
├── shadps4/       # PS4
├── xenia/         # Xbox 360
└── yuzu/          # Switch
```

**Exceptions where the emulator still writes to its own Flatpak directory** despite the central tree existing:

| Emulator | Where it actually writes (observed) |
| --- | --- |
| RetroArch (Flatpak `org.libretro.RetroArch`) | `~/.var/app/org.libretro.RetroArch/config/retroarch/saves/*.srm` — the `.srm` files there have real timestamps (2024-2026); `$emulationPath/saves/retroarch/` is empty. EmuDeck does **not** redirect RA's `savefile_directory` on this install. |
| Dolphin (Flatpak `org.DolphinEmu.dolphin-emu`) | `~/.var/app/org.DolphinEmu.dolphin-emu/data/dolphin-emu/{GC,Wii,GBA/Saves}` — populated with real data. |
| PPSSPP (Flatpak `org.ppsspp.PPSSPP`) | `~/.var/app/org.ppsspp.PPSSPP/config/ppsspp/PSP/SAVEDATA` — populated. |
| Citra (Flatpak `org.citra_emu.citra`) | `~/.var/app/org.citra_emu.citra/data/citra-emu/states` — populated. |
| PCSX2 (AppImage, not Flatpak) | `~/.config/PCSX2/{memcards,sstates}` — populated. The central `$emulationPath/saves/pcsx2/{saves,states}` *does* contain data too on this install (TODO: confirm whether PCSX2 is symlinked or writes both). |

This is the **single most important finding for Phase 4 (save-path resolver)**: a Frontend Protocol method `save_root(system)` returning the central tree is necessary but **not sufficient**. The per-emulator resolver in Phase 4 needs a rule per emulator stating whether the central path is canonical or whether the Flatpak/native dir is. The above table is the seed for `domain/emulator_save_rules.py`.

A pragmatic v1 stance for the EmuDeck adapter's `save_root(system)`: **return the central path** (`$emulationPath/saves/<emu>/`) and let Phase 4's per-emulator rules override on a case-by-case basis. The central path is *also* where EmuDeck's own save-sync tooling expects to find files, so writing there is the safest universal default even when the emulator hasn't been wired to read from it yet.

## RetroArch (Flatpak)

```text
~/.var/app/org.libretro.RetroArch/config/retroarch/
├── retroarch.cfg                       # canonical RA config
├── cores/*.so                          # 130+ cores observed
├── config/<CoreName>/                  # per-core configs
├── saves/                              # default savefile_directory (see Saves table)
├── states/
├── system/                             # core-specific assets (Mupen64 textures, etc.)
├── playlists/, thumbnails/, shaders/, overlays/, …
```

- `retroarch_config_path()` → `~/.var/app/org.libretro.RetroArch/config/retroarch/retroarch.cfg`
- `retroarch_cores_root()` → `~/.var/app/org.libretro.RetroArch/config/retroarch/cores`

Per-core config dirs (e.g. `config/melonDS DS/`) name cores by display string, not by `.so` basename — the existing `RetroArchCoreInfoReader` adapter already handles this for RetroDECK and should work unchanged.

## ES-DE / EmulationStation Desktop Edition

Native install (not Flatpak), rooted at `~/ES-DE/`:

```text
~/ES-DE/
├── settings/                # es_settings.xml lives here
├── gamelists/<system>/       # gamelist.xml per system
├── custom_systems/           # user-defined systems
├── collections/              # custom collections (Phase 7 stretch)
├── themes/<theme-name>/      # downloaded themes
├── scrapers/, screensavers/, controllers/, logs/, scripts/
```

Compare RetroDECK: RetroDECK bundles ES-DE inside its Flatpak sandbox at `~/.var/app/net.retrodeck.retrodeck/config/ES-DE/`. EmuDeck's ES-DE is a normal Linux app at `~/ES-DE/`. The Frontend Protocol method that locates ES-DE config (if added in Phase 6/7) needs to differ between the two adapters.

## Steam ROM Manager (SRM) launchers

SRM is how EmuDeck wires emulator launches into Steam shortcuts. Two relevant trees:

1. **Per-emulator launcher scripts** (the file SRM points each Steam shortcut at):

    ```text
    ~/.config/EmuDeck/backend/tools/launchers/
    ├── retroarch.sh        ← e.g. shortcut.exe target for any RetroArch ROM
    ├── pcsx2-qt.sh
    ├── dolphin-emu.sh
    ├── duckstation.sh
    ├── ppsspp.sh
    ├── rpcs3.sh
    ├── cemu.sh, cemu-native.sh
    ├── ryujinx.sh
    ├── yuzu.sh / suyu.sh / eden.sh / citron.sh   # forks
    ├── citra.sh / lime3ds.sh / azahar.sh         # 3DS forks
    ├── melonds.sh, mgba.sh, scummvm.sh, mame.sh, flycast.sh
    ├── shadps4.sh, primehack.sh, bigpemu.sh
    ├── model-2-emulator.sh, supermodel.sh, ares-emu.sh
    ├── rosaliesmupengui.sh
    ├── es-de/   (subdir with ES-DE-specific helpers)
    ├── pegasus/ (subdir)
    └── srm/     (subdir with SRM-specific helpers)
    ```

    Phase 6 (`launch_command()` for EmuDeck) calls one of these scripts plus the absolute ROM path. The exact argv shape per launcher needs a script-by-script audit before Phase 6 — TODO.

2. **SRM parser configs** (per-system rules: which launcher to use, which ROM dir to scan, what artwork to attach):

    ```text
    ~/.config/EmuDeck/backend/configs/steam-rom-manager/userData/parsers/
    ```

    Phase 6 reads — and arguably writes — entries here when toggling "Create Steam shortcuts" per-platform. Out of scope until then.

3. **SRM's own state** (Electron app — caches, cookies, parsers UI state):

    ```text
    ~/.config/steam-rom-manager/
    ```

    Not relevant to the plugin.

## Native (non-Flatpak) emulator config dirs

Some EmuDeck-installed emulators are AppImages, not Flatpaks, and write config under `~/.config/`. Observed on this install:

| Emulator | Config dir |
| --- | --- |
| PCSX2 | `~/.config/PCSX2/{inis,memcards,sstates,bios,…}` |
| Cemu | `~/.config/Cemu/controllerProfiles/` |
| yuzu | `~/.config/yuzu/input/` |
| Ryujinx | `~/.config/Ryujinx/{system,bis/{system,user},Logs,profiles}` |
| citra-emu | `~/.config/citra-emu/` (also has a parallel Flatpak install) |
| rpcs3 | `~/.config/rpcs3/{input_configs/global,GuiConfigs}` (also Flatpak — Flatpak is the canonical one) |

The duplication (citra + RPCS3 having both Flatpak and native dirs) is install-history-dependent — EmuDeck switched some emulators between Flatpak and AppImage across versions. The Frontend Protocol should prefer the Flatpak path when both exist, and the per-emulator save-rule registry (Phase 4) needs an explicit ordering. **TODO**: confirm preference order against EmuDeck source.

## Open TODOs (need a second sample or an EmuDeck-source dive)

- [ ] Pin down where EmuDeck's own "we are vX.Y" string lives, if anywhere — confirm B11 should use the git ref rather than fishing for a version number.
- [ ] Confirm whether BIOS writes to `$emulationPath/bios/` are mirrored to `bios/citra-flatpak/` automatically, or whether the plugin needs to do that itself.
- [ ] Confirm whether the central `$emulationPath/saves/<emu>/` tree or the per-Flatpak save dir is canonical per-emulator. Current best guess in the [Saves table](#saves-the-central-tree-and-the-flatpak-exceptions) is observation-only and needs cross-checking against `~/.config/EmuDeck/backend/functions/EmuScripts/`.
- [ ] Audit per-launcher argv shape under `~/.config/EmuDeck/backend/tools/launchers/` before Phase 6 lands `EmuDeckFrontendAdapter.launch_command()`.
- [ ] Re-sample on an internal-SSD install (no SD card) to confirm `settings.sh` formatting when `emulationPath="/home/deck/Emulation"`.
