#!/usr/bin/env bash
# Builds a stand-in Steam Deck home under dev/.data/home with EmuDeck and ES-DE laid out the way
# they are on a Deck, for the dev shell's backend to probe. Every run starts from scratch.
#
# The emulators are left in a mix of states so the setup status page has something to flag:
# Dolphin isn't installed, and melonDS's save folder is a dangling link.
#
#   --no-emudeck      leave out the Emulation folder and EmuDeck's settings
#   --no-roms         keep EmuDeck's settings but leave out roms/, as if it was renamed
#   --gamelist-only   turn on ES-DE's "only show games from gamelist files"
#   --no-esde         leave out ES-DE's settings
#
# Then press Check again on the setup status page (or restart the backend). "ES-DE running" stays
# off: the plugin finds ES-DE through /proc, which macOS doesn't have.

set -euo pipefail

home="$(cd "$(dirname "$0")" && pwd)/.data/home"
emudeck=1 roms=1 gamelist_only=false esde=1
for arg in "$@"; do
  case "$arg" in
    --no-emudeck) emudeck=0 ;;
    --no-roms) roms=0 ;;
    --gamelist-only) gamelist_only=true ;;
    --no-esde) esde=0 ;;
    *)
      echo "unknown option: $arg" >&2
      exit 2
      ;;
  esac
done

rm -rf "$home"
mkdir -p "$home"

flatpak() { mkdir -p "$home/.local/share/flatpak/app/$1/current/active"; }
app() {
  mkdir -p "$(dirname "$home/Applications/$1")"
  : >"$home/Applications/$1"
  chmod +x "$home/Applications/$1"
}

# Emulators, installed the way EmuDeck installs them.
flatpak org.libretro.RetroArch
flatpak org.ppsspp.PPSSPP
flatpak net.kuribo64.melonDS
flatpak org.scummvm.ScummVM
app DuckStation.AppImage
app pcsx2-Qt.AppImage
# The dev backend is built for this machine, so on Apple silicon ps2 defaults to ARMSX2.
app armsx2.AppImage
app rpcs3.AppImage
app azahar.AppImage
app Cemu.AppImage
app ES-DE.AppImage
app publish/Ryujinx

if [ "$emudeck" = 1 ]; then
  em="$home/Emulation"
  mkdir -p "$home/.config/EmuDeck"
  # EmuDeck writes $HOME literally; bash, and the plugin, expand it.
  cat >"$home/.config/EmuDeck/settings.sh" <<'EOF'
emulationPath=$HOME/Emulation
romsPath=$HOME/Emulation/roms
toolsPath=$HOME/Emulation/tools
biosPath=$HOME/Emulation/bios
savesPath=$HOME/Emulation/saves
storagePath=$HOME/Emulation/storage
system="linux"
EOF
  # shellcheck disable=SC2016
  echo '{"storagePath": "$HOME"}' >"$home/.config/EmuDeck/settings.json"

  mkdir -p "$em/bios" "$em/storage" "$em/tools/downloaded_media"
  if [ "$roms" = 1 ]; then
    for sys in psx ps2 ps3 snes nes gba gb gbc n64 nds n3ds gc wii psp switch scummvm; do
      mkdir -p "$em/roms/$sys"
    done
    mkdir -p "$em/roms/wiiu/roms" "$em/roms/xbox360/roms"
  fi

  # Saves: RetroArch links into its Flatpak's own folders; DuckStation keeps real folders;
  # melonDS's link dangles, as when its setup never ran on this install.
  ra="$home/.var/app/org.libretro.RetroArch/config/retroarch"
  mkdir -p "$ra/saves" "$ra/states" "$em/saves/retroarch"
  ln -s "$ra/saves" "$em/saves/retroarch/saves"
  ln -s "$ra/states" "$em/saves/retroarch/states"
  mkdir -p "$em/saves/duckstation/saves" "$em/saves/duckstation/states"
  mkdir -p "$em/saves/melonds"
  ln -s "$home/.var/app/net.kuribo64.melonDS/data/melonDS/gone" "$em/saves/melonds/saves"
  mkdir -p "$em/saves/melonds/states"
fi

if [ "$esde" = 1 ]; then
  mkdir -p "$home/ES-DE/settings"
  cat >"$home/ES-DE/settings/es_settings.xml" <<EOF
<?xml version="1.0"?>
<bool name="ParseGamelistOnly" value="$gamelist_only" />
<string name="MediaDirectory" value="$home/Emulation/tools/downloaded_media" />
<string name="ROMDirectory" value="" />
EOF
fi

echo "fake Deck home at $home"
