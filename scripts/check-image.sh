#!/bin/sh
set -eu

image=${1:-wddl:test}

docker run --rm "$image" version
docker run --rm "$image" --help >/dev/null
docker run --rm --workdir /tmp "$image" version >/dev/null
docker run --rm --entrypoint /bin/sh "$image" -c '
  command -v wddl >/dev/null
  command -v busybox >/dev/null
  test ! -e /srv/app
  test "$(id -u)" = 10001
  test "$(id -g)" = 10001
'

check_root=$(mktemp -d)
trap 'rm -rf "$check_root"' EXIT
mkdir -p "$check_root/write" "$check_root/read"
chmod 0777 "$check_root/write"
chmod 0555 "$check_root/read"

docker run --rm --entrypoint /bin/sh \
  --mount "type=bind,src=$check_root/write,dst=/write" \
  --mount "type=bind,src=$check_root/read,dst=/read,readonly" \
  "$image" -c 'touch /write/probe && ! touch /read/probe'

docker run --rm --read-only --cap-drop ALL --security-opt no-new-privileges:true \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m,mode=1777 \
  "$image" version >/dev/null
