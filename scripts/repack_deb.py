#!/usr/bin/env python3
"""Rebuild an OrcheRoute deb from a known-good package layout.

The script preserves Unix metadata from the template archive, replaces the
portable Go runtime and WebUI, refreshes maintainer scripts/md5sums, and writes
a regular ar-based .deb. It intentionally leaves the bundled Mihomo/Desktop
binary untouched unless an explicit replacement is supplied.
"""

from __future__ import annotations

import argparse
import hashlib
import io
import lzma
import os
from pathlib import Path, PurePosixPath
import tarfile
import time


AR_MAGIC = b"!<arch>\n"


def read_ar(path: Path) -> dict[str, bytes]:
    payload = path.read_bytes()
    if not payload.startswith(AR_MAGIC):
        raise ValueError(f"{path} is not an ar archive")
    result: dict[str, bytes] = {}
    offset = len(AR_MAGIC)
    while offset < len(payload):
        header = payload[offset : offset + 60]
        if len(header) != 60 or header[58:60] != b"`\n":
            raise ValueError("invalid ar member header")
        name = header[:16].decode("ascii").strip().rstrip("/")
        size = int(header[48:58].decode("ascii").strip())
        offset += 60
        result[name] = payload[offset : offset + size]
        offset += size + (size % 2)
    return result


def write_ar(path: Path, members: list[tuple[str, bytes]]) -> None:
    with path.open("wb") as stream:
        stream.write(AR_MAGIC)
        timestamp = int(time.time())
        for name, payload in members:
            header = (
                f"{name + '/':<16}{timestamp:<12}{0:<6}{0:<6}{0o100644:<8o}{len(payload):<10}`\n"
            ).encode("ascii")
            if len(header) != 60:
                raise AssertionError((name, len(header)))
            stream.write(header)
            stream.write(payload)
            if len(payload) % 2:
                stream.write(b"\n")


def read_tar(payload: bytes) -> dict[str, tuple[tarfile.TarInfo, bytes | None]]:
    result: dict[str, tuple[tarfile.TarInfo, bytes | None]] = {}
    with tarfile.open(fileobj=io.BytesIO(payload), mode="r:*") as archive:
        for member in archive.getmembers():
            name = normalize(member.name)
            # The archive root (usually "./") is metadata, not a package path.
            # Keeping it under an empty key later produced a literal "/" member,
            # which dpkg correctly rejects as an empty filename.
            if name == "":
                continue
            data = archive.extractfile(member).read() if member.isfile() else None
            member.name = name
            result[name] = (member, data)
    return result


def normalize(value: str) -> str:
    value = value.replace("\\", "/")
    while value.startswith("./"):
        value = value[2:]
    if value in {"", "."}:
        return ""
    if value.startswith("/"):
        raise ValueError(f"absolute archive path is forbidden: {value!r}")
    normalized = str(PurePosixPath(value))
    if normalized == ".." or normalized.startswith("../"):
        raise ValueError(f"parent archive path is forbidden: {value!r}")
    return normalized


def regular(name: str, payload: bytes, mode: int = 0o644) -> tuple[tarfile.TarInfo, bytes]:
    info = tarfile.TarInfo(name)
    info.size = len(payload)
    info.mode = mode
    info.uid = info.gid = 0
    info.uname = info.gname = "root"
    info.mtime = int(time.time())
    return info, payload


def directory(name: str) -> tuple[tarfile.TarInfo, None]:
    info = tarfile.TarInfo(name.rstrip("/") + "/")
    info.type = tarfile.DIRTYPE
    info.mode = 0o755
    info.uid = info.gid = 0
    info.uname = info.gname = "root"
    info.mtime = int(time.time())
    return info, None


def add_tree(members: dict[str, tuple[tarfile.TarInfo, bytes | None]], prefix: str, root: Path) -> None:
    for name in list(members):
        if name == prefix or name.startswith(prefix + "/"):
            del members[name]
    members[prefix] = directory(prefix)
    for current_root, directories, files in os.walk(root):
        directories.sort()
        files.sort()
        relative = Path(current_root).relative_to(root)
        archive_root = PurePosixPath(prefix, *relative.parts)
        if relative.parts:
            members[str(archive_root)] = directory(str(archive_root))
        for filename in files:
            local = Path(current_root, filename)
            name = str(archive_root / filename)
            members[name] = regular(name, local.read_bytes())


def ensure_parent_directories(members: dict[str, tuple[tarfile.TarInfo, bytes | None]]) -> None:
    """Make the archive installable on a host where no parent paths exist."""
    missing: set[str] = set()
    for name in list(members):
        parent = PurePosixPath(name).parent
        while str(parent) not in {"", "."}:
            normalized = str(parent)
            if normalized not in members:
                missing.add(normalized)
            parent = parent.parent
    for name in sorted(missing, key=lambda item: (item.count("/"), item)):
        members[name] = directory(name)


def relocate_systemd_units(members: dict[str, tuple[tarfile.TarInfo, bytes | None]]) -> None:
    """Keep packages safe on merged-/usr systems.

    A raw extraction of a package containing a top-level ``lib`` directory can
    replace the host's ``/lib -> usr/lib`` symlink. Native systemd units belong
    under /usr/lib, which is also accepted by Debian, Astra and Ubuntu.
    """
    source = "lib/systemd/system"
    target = "usr/lib/systemd/system"
    moved: dict[str, tuple[tarfile.TarInfo, bytes | None]] = {}
    for name in list(members):
        if name == source or name.startswith(source + "/"):
            suffix = name[len(source):].lstrip("/")
            destination = target + (("/" + suffix) if suffix else "")
            info, payload = members.pop(name)
            info.name = destination
            moved[destination] = (info, payload)
    members.update(moved)
    for name in ("lib/systemd", "lib"):
        if name in members and not any(item.startswith(name + "/") for item in members):
            del members[name]


def tar_xz(members: dict[str, tuple[tarfile.TarInfo, bytes | None]]) -> bytes:
    ensure_parent_directories(members)
    if "" in members:
        raise ValueError("empty archive member name")
    raw = io.BytesIO()
    with tarfile.open(fileobj=raw, mode="w", format=tarfile.GNU_FORMAT) as archive:
        for name in sorted(members, key=lambda item: (item.count("/"), item)):
            info, payload = members[name]
            info.name = name + ("/" if info.isdir() and not name.endswith("/") else "")
            archive.addfile(info, io.BytesIO(payload) if payload is not None else None)
    return lzma.compress(raw.getvalue(), preset=6)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--template", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--control-dir", type=Path, required=True)
    parser.add_argument("--webui", type=Path, required=True)
    parser.add_argument("--replace", action="append", default=[], metavar="ARCHIVE_PATH=LOCAL_PATH")
    args = parser.parse_args()

    source = read_ar(args.template)
    control_name = next(name for name in source if name.startswith("control.tar"))
    data_name = next(name for name in source if name.startswith("data.tar"))
    control = read_tar(source[control_name])
    data = read_tar(source[data_name])
    relocate_systemd_units(data)
    # Templates from early manual builds may contain debug copies such as
    # r11-postinst. They are not package metadata and must not leak forward.
    allowed_control = {"control", "preinst", "postinst", "prerm", "postrm", "conffiles", "md5sums"}
    control = {name: value for name, value in control.items() if name in allowed_control}

    replacements: dict[str, Path] = {}
    for value in args.replace:
        archive_name, local_name = value.split("=", 1)
        replacements[normalize(archive_name)] = Path(local_name)
    for name, local in replacements.items():
        old = data.get(name)
        mode = old[0].mode if old else 0o755
        data[name] = regular(name, local.read_bytes(), mode)

    add_tree(data, "opt/orcheroute/webui", args.webui)

    for filename in ("control", "preinst", "postinst", "prerm", "postrm", "conffiles"):
        local = args.control_dir / filename
        if local.exists():
            control[filename] = regular(filename, local.read_bytes(), 0o755 if filename in {"preinst", "postinst", "prerm", "postrm"} else 0o644)
        elif filename in control and filename != "control":
            del control[filename]

    sums = []
    for name, (info, payload) in sorted(data.items()):
        if info.isfile() and payload is not None:
            sums.append(f"{hashlib.md5(payload).hexdigest()}  {name}")
    control["md5sums"] = regular("md5sums", ("\n".join(sums) + "\n").encode(), 0o644)

    args.output.parent.mkdir(parents=True, exist_ok=True)
    write_ar(args.output, [
        ("debian-binary", b"2.0\n"),
        ("control.tar.xz", tar_xz(control)),
        ("data.tar.xz", tar_xz(data)),
    ])
    print(args.output)


if __name__ == "__main__":
    main()
