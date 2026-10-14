# Save sync

The plugin keeps your saves in sync with RomM, so you can pick up a game on another device where you
left off on the Deck, and back again. RomM decides what each sync does; the plugin uploads,
downloads, and never resolves a conflict on its own.

## What syncs

Saves sync for games the plugin downloaded, and for games you've linked to RomM (see
[Managing games](managing-games.md#games-you-already-have)), when they're played in one of these
emulators:

| Emulator    | Systems (EmuDeck's defaults)                    | Save                       |
| ----------- | ----------------------------------------------- | -------------------------- |
| RetroArch   | Most cartridge and classic systems, N64, GBA    | `.srm` battery save        |
| melonDS     | Nintendo DS                                     | `.sav` battery save        |
| mGBA        | Game Boy Advance, when ES-DE runs it standalone | `.sav` battery save        |
| RMG         | Nintendo 64, when ES-DE runs it                 | N64 save files, as one zip |
| DuckStation | PlayStation                                     | The game's memory card     |

The emulator is the one ES-DE launches the game with, so changing a system's or a game's emulator in
ES-DE changes which saves sync. Games played in other emulators, such as Dolphin, PCSX2, PPSSPP or
Ryujinx, don't sync their saves; their page says so.

Save states don't sync, only in-game saves.

## When saves sync

- **When you save in a game**: the save uploads a few seconds after the emulator finishes writing
  it.
- **Every 15 minutes**, to pick up saves from your other devices.
- **When you open the panel**, unless a sync just ran.
- **When you ask**: **Sync all saves** in the panel's Saves section, or **Sync saves now** on a
  game's page.

Syncs that run on their own stay quiet unless they download a newer save or find a conflict.

### Downloads wait for the emulator

An emulator writes the save it holds when a game closes, which would undo a download made while the
game runs. So when RomM has a newer save for a game that's running, the download waits, and the game
shows **Waiting for \<emulator\> to close**. It downloads as soon as the emulator closes.

## Conflicts

A conflict means a game's save changed both on the Deck and on RomM since they last matched. Neither
copy is overwritten. The panel lists games in conflict under **⚠ Save conflicts**; open one to
compare the two saves and choose:

- **Keep this Deck's save**: it becomes RomM's newest, so your other devices get it. RomM keeps its
  other versions.
- **Use RomM's save**: it replaces the Deck's, after the plugin backs up the Deck's copy.

While a conflict is open, the Deck's copy is also kept on RomM, outside its save slots, so it's safe
even before you choose.

## Save slots

RomM keeps each game's saves in slots, each with its own line of saves. The plugin syncs the
**autosave** slot by default, the one RomM's web player and other apps share.

**Save slot** on a game's page picks another slot or starts a new one. Switching syncs the game's
save first, then the newest save in the chosen slot replaces the one on the Deck, which is backed
up. An empty slot starts from the Deck's save. Nothing is deleted.

## Backups and history

Whenever a sync, a restore or a slot switch replaces a save on the Deck, the old one is backed up
first. The five newest backups of each game are kept. **Save backups** on a game's page lists them,
and restoring one puts it back and syncs it like any other change.

**Save history** on a game's page, and **Recent activity** in the panel, show what save sync did:
uploads, downloads, conflicts, restores and more.

## When an emulator's settings stop sync

The plugin names saves the way each emulator does, which some emulator settings change. When one of
those settings is changed, that emulator's sync pauses and the panel shows **⚠ \<Emulator\> sync
paused** with what to change back:

- **RetroArch**: **Write Saves to Content Directory**, either **Sort Saves into Folders** option, or
  no **Save Files** folder.
- **melonDS**: no save folder in **Config → Path settings**.
- **mGBA**: a player 2 to 4 save game picked in the **File** menu, or no save folder.
- **RMG**: **Save filename format** set to the cart's header name, or no save (SRAM) folder.
- **DuckStation**: memory card 1 not **Separate Card Per Game (Title)**, or **Use Single Card For
  Multi-Disc Games** turned off.

When an emulator's save folder moves to another folder that exists, the plugin follows it on its
own.

## Special cases

- **Games kept as archives**: RetroArch and mGBA name the save after the archive, and melonDS after
  the game inside it, so these sync like any other game. melonDS can't tell which file of an archive
  holding several it names its save after, so those don't sync.
- **Multi-disc PlayStation games**: DuckStation gives every disc of a game one memory card. When
  RomM keeps the discs as separate games, the card syncs with one of them, and the others' pages say
  which.
- **Nintendo 64 in RMG**: RMG names saves from its ROM database, so RMG needs to be installed. N64
  saves from other emulators on RomM, such as RetroArch's, load in RMG too.
- **Linked games**: sync like downloaded ones, as long as the linked file is still there.

## What save sync needs

- A sign-in that paired the Deck as a device (every sign-in method in the setup does).
- The RomM permissions `assets.read`, `assets.write`, `devices.read` and `devices.write`.
- Save sync turned on for the Deck in RomM's device settings.
- The emulator's save folder set up by EmuDeck. **Settings → Setup status** shows each one.
