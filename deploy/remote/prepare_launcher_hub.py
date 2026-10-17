"""Prepare a separate candidate gateway config; never starts/restarts/deploys services."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gateway-config", type=Path, required=True)
    parser.add_argument("--hub-config", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--access-file", type=Path, required=True)
    args = parser.parse_args()
    if args.output.exists() or args.access_file.exists():
        raise SystemExit("Output/access file exists; choose new paths to preserve credentials")
    gateway = json.loads(args.gateway_config.read_text(encoding="utf-8-sig"))
    hub = json.loads(args.hub_config.read_text(encoding="utf-8-sig"))
    if not all(hub.get(k) for k in ("admin_listen", "data_dir", "signing_key_file", "profile_file")):
        raise SystemExit("Hub paths and admin listener are required")
    token = secrets.token_urlsafe(36)
    hub["admin_token_sha256"] = hashlib.sha256(token.encode()).hexdigest()
    gateway["launcher_hub"] = hub
    for file, content in ((args.output, json.dumps(gateway, indent=2) + "\n"), (args.access_file, "Admin Token: " + token + "\n")):
        file.parent.mkdir(parents=True, exist_ok=True)
        # Apply owner-only POSIX permissions at creation, before secret bytes are
        # written. Run on the Linux target as the existing gateway service user.
        descriptor = os.open(file, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            handle.write(content)
    print("Candidate configuration and protected access file prepared. No service was changed.")


if __name__ == "__main__":
    main()
