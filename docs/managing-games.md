# Managing games

The plugin keeps track of which games it downloaded, so it can update and delete them safely and
sync their saves. It never touches a file it didn't download unless you hand it over.

## How the plugin sees your games

- **Installed**: the plugin downloaded it. It can update it, delete it and sync its saves.
- **On Deck (not managed)**: a copy the plugin didn't download sits where the game would go. The
  plugin leaves it alone, but you can adopt it or link it.
- **Linked**: a copy of yours, linked to RomM so its saves sync. The plugin never deletes or
  replaces it.
- **Removed from RomM**: RomM no longer has the game. The copy on the Deck stays until you delete
  it, and its saves no longer sync.

### When you change a game's files yourself

On each library refresh, the plugin checks the games it downloaded are as it left them. If all of a
game's files are gone, it goes back to **On RomM**. If any changed, or some are missing, the plugin
stops managing it: what's left is yours, and you'll see a notice saying so.

## Deleting a game

**Delete ROM** on a game's page removes the files the plugin downloaded. Before deleting anything,
it checks every file is unchanged since the download. If one changed, nothing is deleted and the
plugin stops managing the game instead.

Saves are kept unless you turn on **Delete saves too**. That moves the save into the game's backups,
and only when RomM holds the save exactly as it is on the Deck. If it doesn't, the option is greyed
out with the reason, usually **Sync first**.

**Delete all installed**, in a platform's list with the **Installed** filter, deletes every game the
plugin downloaded for that platform. It always keeps saves.

ES-DE keeps its gamelist entries for deleted games, with playtime and favorites. To clear them, run
**Utilities → Orphaned data cleanup** in ES-DE.

## Stop managing

**Stop managing** keeps a game's files where they are, but the plugin no longer updates or deletes
them, and its saves stop syncing.

## Updates

When a game's files change on RomM (a new revision, a fixed dump, an added disc), the game shows **⟳
Update available**. The plugin checks the games that changed on each library refresh, and every game
it downloaded once a day.

- **Update** downloads the new files and swaps them in place of the old ones. Until the swap, the
  old version stays playable.
- **Ignore this update** keeps the version you have. The plugin offers an update again only when
  RomM's files change again.

Before an update, the game's save syncs with RomM. If the update renames the game, and so the name
its emulator gives its save, the save moves to the new name and a copy stays in the game's backups.

## Games you already have

### On a game's page

When a game shows **On Deck (not managed)**, the plugin compares your copy with RomM's:

- If it matches, **Adopt** hands it to the plugin, to update and delete like a game it downloaded.
- Either way, **Link to RomM** syncs its saves while the file stays yours. **Unlink** undoes it.

### Find my games

**Find my games**, in a platform's list or full-screen view, looks through the platform's ROM folder
and matches your files against RomM by their content, whatever they're named. Nothing changes until
you choose:

- **Adopt** or **Link to RomM** for each game found
- **Adopt all** to adopt every match at once

Games the plugin already manages or has linked aren't listed.

## Games RomM no longer has

When a game disappears from RomM, its page says **Removed from RomM**. Your copy stays until you
delete it. If the game comes back on RomM under a new entry, for example after a re-import, the
plugin finds it again and carries on as before.
