#!/bin/sh
set -e

# decky CLI runs this container as the invoking user with HOME=/, which isn't writable.
export GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod

cd /backend
make
