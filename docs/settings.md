# Settings and plugin data

**Settings**, at the bottom of the plugin's panel, holds preferences, the server, EmuDeck's status
and the plugin's own data.

## Preferences

All three are on by default.

- **Download BIOS with games**: a platform's first game download also fetches its missing BIOS
  files. Files already in the BIOS folder are never replaced.
- **Wait for games to close**: while an emulator is running, downloaded games wait to be extracted
  until it closes, so the game doesn't stutter. Downloads keep going.
- **Add art to ES-DE**: downloaded games get their RomM cover and screenshot in ES-DE without
  scraping.

Per-platform choices, **Install to** and **Keep archives**, are in each platform's list. See
[Downloading games](downloads.md).

## The server

**Server settings** opens the server page:

- **Change address**: for when RomM moved to a new address. The plugin checks it's the same server
  by looking up some of your games there, and keeps everything. You need to be signed in.
- **Forget server**: drops the sign-in, the library and the plugin's records of the games it
  downloaded, and cancels the queue. Every file stays on the Deck, yours to manage: the plugin won't
  update or delete them. Forget the server to switch to a different RomM server.

## Signing out

**Sign out** forgets the Deck's sign-in. Downloads, save sync and library refreshes pause until you
sign in again. The server, your games and their records stay, so signing in again carries on where
you left off.

To sign back in, press **Sign in again** in the panel's RomM section. The same happens when RomM
stops accepting the Deck's sign-in, for example when you remove its token in RomM.

## EmuDeck and ES-DE

The **EmuDeck** section shows where the plugin found EmuDeck's `Emulation` folder, and flags ES-DE
settings that would hide downloaded games. **Setup status** checks everything in detail; see
[Getting started](getting-started.md#checking-your-setup).

## Backend

The plugin does its work in a background process. The **Backend** section shows whether it's running
and its version, and offers **Restart backend** if it stopped.

## Plugin data

- **Clear cache**: reloads the library and covers from RomM. Downloads, saves and settings stay.
  Useful when the library looks out of date or covers look wrong.
- **Reset plugin data**: the plugin forgets everything: its settings, its sign-in, which games it
  downloaded, their save backups, and unfinished downloads. Games and saves stay on the Deck, but
  the plugin no longer manages them. The Deck stays a device on RomM; you can remove it there.

### Restore backup

The plugin backs up its records before each update that changes how it stores them. If you go back
to an older version of the plugin, it can't read records written by the newer one, and the panel's
Downloads section shows **⚠ Downloads are off** with **Restore backup**. Restoring brings back the
copy from before the update; games downloaded or changed since may no longer be managed. The newer
records are kept aside, for updating the plugin again.

## Uninstalling

Uninstalling the plugin removes its caches, downloads that never finished, and its sign-in. It keeps
its settings and its records of the games it downloaded, so if you install it again and sign in to
the same server, it still manages them. Games and saves are never removed.

To remove everything instead, use **Reset plugin data** before uninstalling.
