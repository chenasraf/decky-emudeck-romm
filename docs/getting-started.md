# Getting started

## Before you begin

- **EmuDeck** installed and set up on the Deck, with ES-DE. The plugin finds EmuDeck's `Emulation`
  folder on its own, whether it's on the internal drive or an SD card.
- **A RomM server**, version 5.1.0 or newer, that the Deck can reach on your network or over the
  internet.
- **[Decky Loader](https://decky.xyz)** installed.

## Installing

1. Download `decky-emudeck-romm.zip` from the
   [latest release](https://github.com/chenasraf/decky-emudeck-romm/releases/latest).
2. In Decky's settings, turn on **Developer mode**, then in the **Developer** tab choose **Install
   Plugin from ZIP** and pick the file.
3. Open the Quick Access Menu (the **···** button) and choose **EmuDeck RomM Sync**.

## Connecting to RomM

Press **Set up RomM** in the plugin's **RomM** section.

### 1. The server address

Enter the address you open RomM at in a browser, such as `romm.example.com` or `192.168.1.20:8080`,
and press **Continue**. Without `http://` or `https://`, the plugin tries HTTPS first, then HTTP.

If your server uses a self-signed certificate, the plugin shows its SHA-256 fingerprint and a
**Trust this certificate** button. Trust it only if the fingerprint matches the one your server
uses: the plugin remembers it, and refuses the server if its certificate ever changes.

### 2. Signing in

There are three ways to sign in. Each adds the Deck to RomM as a device, which save sync needs.

- **QR code** (the default). Scan the code with your phone, sign in to RomM if asked, and approve
  the Deck. You can also open the address shown under the code on any device and enter the code
  there.
- **Pair code**. In RomM, open your profile, go to **Client API Tokens**, create a token (or
  regenerate one) and choose **Pair Device**. Enter the 8-character code before its countdown runs
  out. Dashes, spaces and letter case don't matter.
- **Password**. Sign in with your RomM username and password. The plugin uses the password once to
  add the Deck as a device, then forgets it. Your account needs the `me.write` permission.

Switch between them with the **Use a QR code**, **Use a pair code** and **Use a password** buttons.

### 3. Done

The plugin shows the server and its RomM version. If RomM didn't grant some permissions, the
features that need them are listed as off.

| Feature          | Needs these RomM permissions                                   |
| ---------------- | -------------------------------------------------------------- |
| Library browsing | `platforms.read`, `roms.read`                                  |
| ROM downloads    | `roms.read`                                                    |
| Collections      | `collections.read`                                             |
| BIOS downloads   | `firmware.read`                                                |
| Save sync        | `assets.read`, `assets.write`, `devices.read`, `devices.write` |

To turn a feature on, sign in again and allow the permissions it needs.

## The Quick Access Menu

Once you're signed in, the plugin's panel has these sections, top to bottom:

- **Library**: how many games RomM has, when the library last refreshed, and the ways in: **Open
  RomM library**, **Search**, **Quick browse** and **Collections**. See
  [Browsing the library](library.md).
- **RomM**: whether the server is connected, offline or signed out.
- **Downloads**: the download queue, shown while anything is in it.
- **Saves**: where save sync stands, with **Sync all saves**. See [Save sync](save-sync.md).
- **Settings**: preferences, server settings, EmuDeck's status and plugin data. See
  [Settings and plugin data](settings.md).

Opening the panel refreshes the library when it's more than five minutes old, and syncs saves unless
a sync ran in the last two minutes.

## Checking your setup

**Settings → Setup status** checks everything the plugin depends on, and says how to fix anything
that's off:

- **EmuDeck**: where the `Emulation` folder is, and how it was found.
- **ES-DE**: whether its settings exist, whether it reads EmuDeck's ROM folder, and whether it shows
  games that aren't in its gamelists yet.
- **Emulators**: which emulators EmuDeck installed for each system, and whether the save folders of
  the emulators whose saves sync are set up.

When something needs attention, the panel shows **⚠ EmuDeck needs attention** above the Settings
button.
