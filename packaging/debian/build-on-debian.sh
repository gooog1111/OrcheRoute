#!/bin/sh
set -eu

ROOT=${1:?usage: build-on-debian.sh STAGING_ROOT OUTPUT_DEB}
OUTPUT=${2:?usage: build-on-debian.sh STAGING_ROOT OUTPUT_DEB}

chmod 0755 "$ROOT/DEBIAN/postinst" "$ROOT/DEBIAN/prerm" "$ROOT/DEBIAN/postrm"
if [ -f "$ROOT/DEBIAN/preinst" ]; then
    chmod 0755 "$ROOT/DEBIAN/preinst"
fi
chmod 0755 "$ROOT/DEBIAN"
chmod 0644 "$ROOT/DEBIAN/control" "$ROOT/DEBIAN/md5sums"
chmod 0755 "$ROOT/opt/orcheroute/bin/"*
for SYSTEMD_DIR in "$ROOT/lib/systemd/system" "$ROOT/usr/lib/systemd/system"; do
    if [ -d "$SYSTEMD_DIR" ]; then
        chmod 0644 "$SYSTEMD_DIR/"*
    fi
done
test -x "$ROOT/opt/orcheroute/bin/orcheroute-server"
test -x "$ROOT/opt/orcheroute/bin/mihomo"
test -f "$ROOT/opt/orcheroute/webui/index.html"
dpkg-deb --build --root-owner-group -Zxz "$ROOT" "$OUTPUT"
