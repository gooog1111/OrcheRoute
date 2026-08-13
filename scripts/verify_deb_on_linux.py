#!/usr/bin/env python3
"""Upload DEBs to /tmp and validate them with the target host's dpkg-deb.

This never installs a package and never invokes sudo.
"""

from __future__ import annotations

import argparse
from pathlib import Path
import shlex
import uuid

import paramiko


def read_env(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in path.read_text(encoding="utf-8-sig").splitlines():
        line = raw.strip()
        if line and not line.startswith("#") and "=" in line:
            key, value = line.split("=", 1)
            values[key.strip().lower()] = value.strip().strip('"').strip("'")
    return values


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("files", nargs="+", type=Path)
    args = parser.parse_args()

    workspace = Path(__file__).resolve().parents[2]
    connection = read_env(workspace / ".env" / "ssh")
    backup = workspace.parent / "server-backups" / "2026-08-01" / "ssh.env"
    credentials = read_env(backup) if backup.exists() else {}
    host = connection.get("host") or connection.get("hostname") or connection.get("ip")
    port = int(connection.get("port", "22"))
    username = credentials.get("login") or credentials.get("username") or connection.get("login")
    password = credentials.get("password") or connection.get("password")
    if not all((host, username, password)):
        raise SystemExit("SSH credentials are incomplete")

    client = paramiko.SSHClient()
    client.load_system_host_keys()
    client.set_missing_host_key_policy(paramiko.RejectPolicy())
    client.connect(host, port=port, username=username, password=password, timeout=20)
    uploaded: list[str] = []
    try:
        sftp = client.open_sftp()
        try:
            for local in args.files:
                local = local.resolve()
                remote = f"/tmp/orcheroute-verify-{uuid.uuid4().hex}-{local.name}"
                sftp.put(str(local), remote)
                uploaded.append(remote)
        finally:
            sftp.close()

        for remote in uploaded:
            quoted = shlex.quote(remote)
            command = (
                "set -eu; "
                "root=$(mktemp -d /tmp/orcheroute-deb-extract.XXXXXX); "
                "trap 'rm -rf -- \"$root\"' EXIT; "
                f"dpkg-deb --info {quoted} >/dev/null; "
                f"dpkg-deb --contents {quoted} >/dev/null; "
                f"dpkg-deb --extract {quoted} \"$root\"; "
                f"dpkg-deb --field {quoted} Package Version Architecture"
            )
            _, stdout, stderr = client.exec_command(command, timeout=180)
            output = stdout.read().decode("utf-8", "replace").strip()
            error = stderr.read().decode("utf-8", "replace").strip()
            code = stdout.channel.recv_exit_status()
            if code != 0 or error:
                raise RuntimeError(f"dpkg-deb rejected {Path(remote).name}: code={code} stderr={error}")
            print(f"{Path(remote).name}: {output} dpkg-deb=ok")
    finally:
        if uploaded:
            sftp = client.open_sftp()
            try:
                for remote in uploaded:
                    try:
                        sftp.remove(remote)
                    except FileNotFoundError:
                        pass
            finally:
                sftp.close()
        client.close()


if __name__ == "__main__":
    main()
