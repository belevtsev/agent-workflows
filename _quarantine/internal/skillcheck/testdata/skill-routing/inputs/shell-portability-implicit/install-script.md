# install.sh as published

This is the exact text of the install.sh file that users fetch from the release URL.

```sh
#!/bin/sh
set -e

BASE_URL=${TOOL_BASE_URL:-https://example.invalid/releases/latest}
INSTALL_DIR=${TOOL_INSTALL_DIR:-$HOME/.local/bin}

HERE=$(dirname "$0")
. "$HERE/lib/detect.sh"

platforms=(darwin linux)
os=$(uname -s | tr '[:upper:]' '[:lower:]')
if [[ ! " ${platforms[@]} " =~ " $os " ]]; then
	echo "unsupported operating system: $os" >&2
	exit 1
fi
arch=$(detect_arch)

tmp=$(mktemp -d)
curl -fsSL "$BASE_URL/tool_${os}_${arch}.tar.gz" -o $tmp/tool.tar.gz

if [ -e $INSTALL_DIR/tool ]; then
	printf 'Replace the existing tool? [y/N] '
	read answer
	case $answer in
	y | Y) ;;
	*) exit 0 ;;
	esac
fi

mkdir -p $INSTALL_DIR
tar -xzf $tmp/tool.tar.gz -C $INSTALL_DIR tool
chmod 755 $INSTALL_DIR/tool
echo "installed tool to $INSTALL_DIR"
```
