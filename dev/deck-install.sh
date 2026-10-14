#!/bin/sh
# Installs a staged build into Decky's plugin folder and restarts Decky's loader. `make deploy` runs
# it on the Deck as root, from the deck user's home, with the stage beside it.
#
# Decky's unit stops with KillMode=process, and its loader is a PyInstaller launcher around the real
# one: when any plugin hangs the shutdown past the stop timeout, systemd kills only the launcher, and
# the real loader lives on holding port 1337, so every start after fails and the unit restarts in a
# loop. Whatever is still in the unit's cgroup once it's stopped is such a leftover. It's killed, and
# the start waits until it has exited and let go of the port.
set -u

stage=$1
plugin=/home/deck/homebrew/plugins/decky-emudeck-romm
procs=/sys/fs/cgroup/system.slice/plugin_loader.service/cgroup.procs

systemctl stop plugin_loader
if [ -r "$procs" ] && grep -q . "$procs"; then
  echo "killing Decky's leftover processes: $(tr '\n' ' ' < "$procs")"
  xargs -r kill -9 < "$procs" 2>/dev/null
  i=0
  while grep -q . "$procs" && [ "$i" -lt 40 ]; do
    sleep 0.25
    i=$((i + 1))
  done
fi
rsync -a "$stage/" "$plugin/" && systemctl start plugin_loader
