# FAQ and troubleshooting

## Setup

### "No RomM server answered at this address."

Check you can open the same address in a browser on another device on the same network. Include the
port if RomM uses one (`192.168.1.20:8080`), and try `http://` in front of it if your server doesn't
use HTTPS.

### "The server's certificate isn't trusted."

Your server uses a self-signed or private certificate. Compare the fingerprint the plugin shows with
your server's, and press **Trust this certificate** if they match.

### "The plugin needs RomM 5.1.0 or newer."

Update your RomM server. Older versions can't resume downloads of games made of several files, which
the plugin relies on.

### Signing in with a password fails with a permission error

Signing in a device needs the `me.write` permission on your RomM account. Ask your RomM admin to
grant it, or sign in with a QR code or pair code instead.

### Some features say "Off" after signing in

RomM didn't grant the permissions those features need. Sign in again and allow them; the table in
[Getting started](getting-started.md#3-done) lists what each feature needs.

## Downloads and games

### A downloaded game doesn't show up in ES-DE

- If ES-DE was open while the game downloaded, run **Utilities → Rescan ROM directory** in ES-DE, or
  restart it.
- If **Settings → Setup status** says ES-DE reads games from another folder, point ES-DE back at
  EmuDeck's ROM folder: reset ES-DE's configuration from EmuDeck.
- If ES-DE only shows games from its gamelists, turn off **Other Settings → Only show games from
  gamelist files** in ES-DE.

### A downloaded game doesn't show up in Steam

Run Steam ROM Manager to add it. Games installed to a folder marked **not in Steam ROM Manager** in
**Install to** aren't picked up by it; choose the default folder for those.

### "EmuDeck wasn't found"

The plugin looks for EmuDeck's `Emulation` folder in EmuDeck's settings, then in `~/Emulation`.
Install EmuDeck and finish its setup, then press **Check again** in Settings.

### "... is already in ... The plugin doesn't replace files it didn't download."

A file of yours is where the game would go. The game shows as **On Deck (not managed)**: open it to
adopt it or link it to RomM, or move your file away and download again.

### Downloads are on hold

The drive with the `Emulation` folder is full. Free some space, or delete games you've finished; the
queue continues on its own once there's room.

### A game says "Not ready to play"

RomM couldn't convert the game to a format that plays on the Deck, so it downloaded as stored. The
message says what's left, such as installing a `.cia` in Azahar or an `.nsz` in Ryujinx.

### A game says "On Deck (not managed)" but I downloaded it with the plugin

The game's files changed after the download, so the plugin stopped managing them to avoid deleting
your changes. Adopt it again from its page if its files still match RomM's.

### Does deleting a game delete its saves?

No, unless you turn on **Delete saves too**, and even then only when RomM holds the save exactly as
it is on the Deck. The save moves into the game's backups rather than being removed.

### Can I choose where games download?

For systems with several EmuDeck folders, yes: **Install to** in the platform's list. Otherwise
games go into the system's EmuDeck folder, so ES-DE and Steam ROM Manager find them.

## Save sync

### A game's saves don't sync

Its page says why under **Saves**. The usual reasons:

- The game runs in an emulator whose saves don't sync. See the list in [Save sync](save-sync.md).
- The plugin didn't download the game. Link it to RomM from its page.
- The emulator's save folder isn't set up. **Settings → Setup status** shows each one.
- An emulator setting renames its saves; the panel shows **⚠ \<Emulator\> sync paused** with what to
  change.

### "This Deck signed in without pairing as a device"

Save sync needs the Deck to be a device on RomM. Sign out, then sign in again with any of the
setup's methods.

### "RomM has save sync turned off for this Deck"

Turn save sync on for the Deck in RomM's device settings.

### A newer save from my other device didn't download

If the game is running, the download waits until the emulator closes, since the emulator would
overwrite it on exit. Otherwise press **Sync saves now** on the game's page.

### I got a save conflict

The save changed on both sides since they last matched, for example after playing offline on the
Deck and on another device. Open the game from the panel's **⚠ Save conflicts** list and choose
which save to keep. Neither is lost: the other stays on RomM or in the game's backups.

### I overwrote a save by mistake

Open the game's **Save backups** and restore the one from before. The five newest replaced saves of
each game are kept, and RomM keeps its own history of each slot.

### Do save states sync?

For Game Boy, Game Boy Color and Game Boy Advance games, yes, both ways with RomM's web player: a
state saved in the browser loads on the Deck, and one saved on the Deck loads in the browser, as
long as both play the game in the same emulator core. Other systems sync in-game saves only. See
[Save states](save-sync.md#save-states).

### A game started somewhere other than where I left it

RomM's newest save state goes in the game's auto-state, which RetroArch loads when the game starts.
If a state from the web player or another device was newer than the Deck's, that's where the game
picks up. The Deck's state went to RomM first: open **Browse save states** on the game's page and
load it. To keep RomM's states out of the auto-state, pick another slot under **RomM's state goes
in**.

## Everything else

### Does the plugin work offline?

Yes, for browsing: the library stays available from its copy on the Deck. Downloads, save syncs and
library refreshes wait and resume once the server is reachable.

### Does it run on devices other than the Steam Deck?

It runs on Decky Loader on x86-64 and ARM64 Linux devices with EmuDeck.

### The library looks out of date, or covers are wrong

Press **Refresh library** in the panel. If that doesn't help, **Settings → Clear cache** reloads the
library and covers from RomM.

### How do I move to a different RomM server?

If it's the same server at a new address, use **Server settings → Change address**; everything
carries over. For a different server, **Forget server** first, then set up the new one. Games stay
on the Deck, but the plugin no longer manages the ones it downloaded from the old server.

### How do I start over?

**Settings → Reset plugin data** makes the plugin forget everything. Games and saves stay on the
Deck.

### Where do I report a bug?

On the [issue tracker](https://github.com/chenasraf/decky-emudeck-romm/issues). Decky's logs, found
in `~/homebrew/logs/decky-emudeck-romm/`, help track problems down.
