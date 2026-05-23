// Single source of truth for the fork's user-facing identifiers.
//
// DISPLAY_NAME must equal plugin.json:name (enforced by src/branding.test.ts).
// CLIENT_NAME must equal package.json:name and the RomM device-registry client
// string returned by the backend. Disjoint from upstream's "decky-romm-sync"
// so device records from this fork don't collide with upstream's.

export const DISPLAY_NAME = "EmuDeck RomM Sync";

export const CLIENT_NAME = "decky-emudeck-romm";
