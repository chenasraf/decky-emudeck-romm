# Browsing the library

The plugin keeps a copy of your RomM library on the Deck, so it opens instantly and stays browsable
when the server is offline. It refreshes when you open the panel and the copy is more than five
minutes old, or when you press **Refresh library**.

## In the Quick Access Menu

- **Quick browse** lists your platforms, each with how many of its games are on the Deck
  (`installed/total`).
- **Collections** lists your RomM collections, including smart collections, favorites first and
  marked ★.
- **Search** finds games by title or ROM hash as you type.

A platform or collection opens its games. The filters across the top narrow them:

- **All**
- **Installed**: games on the Deck, including those with an update
- **Not installed**
- **Updates**: games RomM has a newer version of

A platform's list also has **Install to**, **Keep archives**, its BIOS status, **Find my games** and
a search box for that platform. Lists show 50 games at a time; **Show more** loads the next.

## The full-screen library

**Open RomM library** opens a full-screen view built for the Deck's screen and controller. Its
shelves show:

- **Downloading**: what's in the queue
- **Updates available**
- **Recently added**
- **Installed on this Deck**
- one shelf per platform, ending in **See all**, which opens the whole platform with filters,
  sorting (**Name**, **Date added**, **Size**) and **Download all missing**

Press **Y** to search, and **B** to go back.

## Covers

Covers come from RomM and are kept on the Deck; covers of the games you've downloaded are fetched
ahead of time. A game without a cover shows its name on a colored tile.

## Game pages

Selecting a game opens its page: its state, platform, region, revision and size, what you can do
with it, its BIOS status, and its saves once it's on the Deck.

| State                 | Meaning                                                           |
| --------------------- | ----------------------------------------------------------------- |
| On RomM               | Not on the Deck. **Download** gets it.                            |
| Queued / Downloading  | In the download queue. **Cancel** stops it.                       |
| Installed             | Downloaded by the plugin, which can update and delete it.         |
| ⟳ Update available    | RomM has newer files for it.                                      |
| On Deck (not managed) | A copy the plugin didn't download is where this game would go.    |
| Download failed       | **Retry** tries again; **Dismiss** clears it.                     |
| Removed from RomM     | RomM no longer has the game. Your copy stays until you delete it. |

[Managing games](managing-games.md) explains each of these in more detail.

## Searching

Search asks RomM first, and falls back to matching names in the copy on the Deck when RomM doesn't
answer within a few seconds. You can also paste a ROM's hash to find exactly that file.

## Offline

When the server can't be reached, the panel shows **⚠ Offline** and the library stays browsable from
the copy on the Deck. Downloads and save syncs wait and retry on their own, and the library
refreshes once the server is back.
