#!/bin/sh
set -eu

pico_dns_ip=${1:-192.168.68.65}
blocked_host=${2:-}
allowed_exception=${3:-}
podman_config_dir=$(mktemp -d)
mkdir -p "$podman_config_dir/containers"
printf 'unqualified-search-registries = ["docker.io"]\nshort-name-mode = "enforcing"\n' > "$podman_config_dir/containers/registries.conf"
trap 'rm -rf "$podman_config_dir"' EXIT HUP INT TERM

XDG_CONFIG_HOME="$podman_config_dir" podman run --rm \
	--network=pasta \
	--dns="$pico_dns_ip" \
	--dns-option=timeout:2 \
	--dns-option=attempts:1 \
	docker.io/library/alpine:3.22 \
	sh -c '
echo "Container resolver:"
cat /etc/resolv.conf
echo
echo "A lookup through the Pico W:"
nslookup -type=A example.com
echo
echo "AAAA lookup through the Pico W:"
nslookup -type=AAAA example.com

if [ -n "$1" ]; then
  echo
  echo "Block-list check for $1 (expect 0.0.0.0):"
  blocked_result=$(nslookup -type=A "$1")
  printf "%s\n" "$blocked_result"
  printf "%s\n" "$blocked_result" | grep -Fq "Address: 0.0.0.0" || {
    echo "Block-list check failed" >&2
    exit 1
  }
fi

if [ -n "$2" ]; then
  echo
  echo "Allow-list exception check for $2 (expect a real upstream address):"
  allowed_result=$(nslookup -type=A "$2")
  printf "%s\n" "$allowed_result"
  case "$allowed_result" in
    *"Address: 0.0.0.0"*) echo "Allow-list check failed: got sink address" >&2; exit 1 ;;
  esac
  printf "%s\n" "$allowed_result" | sed -n "/^Name:/,\$p" | grep -Eq "^Address: [0-9]" || {
    echo "Allow-list check failed: no upstream address" >&2
    exit 1
  }
fi
' \
	"pico-dns-test" "$blocked_host" "$allowed_exception"
