#!/bin/bash
set -a
. /configs/base/config.sh
if [ -f /configs/spr-masque/config.sh ]; then
  . /configs/spr-masque/config.sh
fi
set +a

# The plugin binary supervises usque (SOCKS5 proxy mode) as a child process.
exec /masque_plugin
