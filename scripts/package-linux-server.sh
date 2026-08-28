#!/bin/sh
set -eu

BASE_DEB=${1:?usage: package-linux-server.sh BASE_DEB OUTPUT_DEB BIN_DIR WEB_DIR PACKAGING_DIR}
OUTPUT_DEB=${2:?usage: package-linux-server.sh BASE_DEB OUTPUT_DEB BIN_DIR WEB_DIR PACKAGING_DIR}
BIN_DIR=${3:?usage: package-linux-server.sh BASE_DEB OUTPUT_DEB BIN_DIR WEB_DIR PACKAGING_DIR}
WEB_DIR=${4:?usage: package-linux-server.sh BASE_DEB OUTPUT_DEB BIN_DIR WEB_DIR PACKAGING_DIR}
PACKAGING_DIR=${5:?usage: package-linux-server.sh BASE_DEB OUTPUT_DEB BIN_DIR WEB_DIR PACKAGING_DIR}

STAGING=$(mktemp -d /tmp/orcheroute-package.XXXXXX)
cleanup() { rm -rf -- "$STAGING"; }
trap cleanup EXIT HUP INT TERM

dpkg-deb -R "$BASE_DEB" "$STAGING"
for binary in orcheroute-server orcheroute-components-go orcheroute-network-go orcheroute-update-go orcheroute-self-update; do
    install -m 0755 "$BIN_DIR/$binary" "$STAGING/opt/orcheroute/bin/$binary"
done

rm -rf -- "$STAGING/opt/orcheroute/webui"
install -d -m 0755 "$STAGING/opt/orcheroute/webui"
cp -a "$WEB_DIR/." "$STAGING/opt/orcheroute/webui/"
find "$STAGING/opt/orcheroute/webui" -type d -exec chmod 0755 {} +
find "$STAGING/opt/orcheroute/webui" -type f -exec chmod 0644 {} +

install -m 0644 "$PACKAGING_DIR/control" "$STAGING/DEBIAN/control"
for script in preinst postinst prerm postrm; do
    if LC_ALL=C grep -q "$(printf '\r')" "$PACKAGING_DIR/$script"; then
        echo "Debian maintainer script uses CRLF: $PACKAGING_DIR/$script" >&2
        exit 1
    fi
    install -m 0755 "$PACKAGING_DIR/$script" "$STAGING/DEBIAN/$script"
done
install -m 0644 "$PACKAGING_DIR/README.Debian" "$STAGING/DEBIAN/README.Debian"
install -d -m 0755 "$STAGING/usr/share/doc/orcheroute"
install -m 0644 "$PACKAGING_DIR/copyright" "$STAGING/usr/share/doc/orcheroute/copyright"
install -m 0644 "$PACKAGING_DIR/../../THIRD_PARTY_NOTICES.md" "$STAGING/usr/share/doc/orcheroute/THIRD_PARTY_NOTICES.md"

(
    cd "$STAGING"
    find . -type f ! -path './DEBIAN/*' -print0 \
        | sort -z \
        | xargs -0 md5sum \
        | sed 's#  \./#  #' > DEBIAN/md5sums
)
sh "$PACKAGING_DIR/build-on-debian.sh" "$STAGING" "$OUTPUT_DEB"
