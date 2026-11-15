#!/bin/sh
# Install a verified release archive without a source checkout or Go.
set -eu

_kitsu_version=${KITSU_VERSION:-v3.0.0-beta.2}
_kitsu_install_dir=${KITSU_INSTALL_DIR:-}
_kitsu_no_modify_path=${KITSU_NO_MODIFY_PATH:-0}
_kitsu_archive_dir=${KITSU_ARCHIVE_DIR:-}
_kitsu_work=
_kitsu_stage=
_kitsu_profile_stage=

usage() {
	printf '%s\n' \
		'Usage: install.sh [--version TAG] [--install-dir DIR] [--no-modify-path] [--archive-dir DIR]' \
		'' \
		'Defaults: version v3.0.0-beta.2; install directory $HOME/.local/bin.' \
		'Environment: KITSU_VERSION, KITSU_INSTALL_DIR, KITSU_NO_MODIFY_PATH=1,' \
		'             KITSU_ARCHIVE_DIR, KITSU_DOWNLOAD_BASE.' \
		'An archive directory must contain the release archive and SHA256SUMS.'
}

fail() {
	printf 'kitsu installer: %s\n' "$1" >&2
	exit 1
}

argument_error() {
	printf 'kitsu installer: %s\n' "$1" >&2
	usage >&2
	exit 2
}

need_value() {
	[ "$#" -ge 2 ] && [ -n "$2" ] || argument_error "$1 requires a value"
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--version)
			need_value "$@"
			_kitsu_version=$2
			shift 2
			;;
		--version=*)
			_kitsu_version=${1#*=}
			[ -n "$_kitsu_version" ] || argument_error '--version requires a value'
			shift
			;;
		--install-dir)
			need_value "$@"
			_kitsu_install_dir=$2
			shift 2
			;;
		--install-dir=*)
			_kitsu_install_dir=${1#*=}
			[ -n "$_kitsu_install_dir" ] || argument_error '--install-dir requires a value'
			shift
			;;
		--archive-dir)
			need_value "$@"
			_kitsu_archive_dir=$2
			shift 2
			;;
		--archive-dir=*)
			_kitsu_archive_dir=${1#*=}
			[ -n "$_kitsu_archive_dir" ] || argument_error '--archive-dir requires a value'
			shift
			;;
		--no-modify-path)
			_kitsu_no_modify_path=1
			shift
			;;
		--help|-h)
			usage
			exit 0
			;;
		--)
			shift
			[ "$#" -eq 0 ] || argument_error "unexpected argument: $1"
			;;
		*)
			argument_error "unknown option: $1"
			;;
	esac
done

case "$_kitsu_version" in
	''|[!a-zA-Z0-9]*|*[!a-zA-Z0-9._+-]*)
		argument_error 'version must be a release tag containing letters, numbers, dots, underscores, + or -'
		;;
esac
case "$_kitsu_no_modify_path" in
	0|1) ;;
	*) argument_error 'KITSU_NO_MODIFY_PATH must be 0 or 1' ;;
esac
if [ -z "$_kitsu_install_dir" ]; then
	[ -n "${HOME:-}" ] || fail 'HOME is unset; specify --install-dir'
	_kitsu_install_dir=$HOME/.local/bin
fi
_kitsu_cr=$(printf '\r')
case "$_kitsu_install_dir" in
	*:*|*'
'*|*"$_kitsu_cr"*)
		fail 'install directory cannot contain a colon or a line break'
		;;
esac

cleanup() {
	[ -z "$_kitsu_profile_stage" ] || rm -f "$_kitsu_profile_stage" || :
	[ -z "$_kitsu_stage" ] || rm -f "$_kitsu_stage" || :
	[ -z "$_kitsu_work" ] || rm -rf "$_kitsu_work" || :
}
trap cleanup 0
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

for _kitsu_command in uname awk tar mktemp cp chmod mv rm mkdir; do
	command -v "$_kitsu_command" >/dev/null 2>&1 ||
		fail "required program not found: $_kitsu_command"
done
if command -v sha256sum >/dev/null 2>&1; then
	_kitsu_checksum=sha256sum
elif command -v shasum >/dev/null 2>&1; then
	_kitsu_checksum=shasum
else
	fail 'SHA-256 verification requires sha256sum or shasum'
fi

_kitsu_os=$(uname -s) || fail 'cannot detect the operating system'
case "$_kitsu_os" in
	Darwin) _kitsu_os=darwin ;;
	Linux) _kitsu_os=linux ;;
	*) fail "unsupported operating system: $_kitsu_os; use install.ps1 on Windows" ;;
esac
_kitsu_arch=$(uname -m) || fail 'cannot detect the processor architecture'
case "$_kitsu_arch" in
	x86_64|amd64) _kitsu_arch=amd64 ;;
	aarch64|arm64) _kitsu_arch=arm64 ;;
	*) fail "unsupported processor architecture: $_kitsu_arch" ;;
esac
# A shell running under Rosetta still installs the native Apple Silicon binary.
if [ "$_kitsu_os" = darwin ] && command -v sysctl >/dev/null 2>&1; then
	_kitsu_native_arm=$(sysctl -n hw.optional.arm64 2>/dev/null || true)
	if [ "$_kitsu_native_arm" = 1 ]; then
		_kitsu_arch=arm64
	fi
fi
_kitsu_archive=kitsu_${_kitsu_version}_${_kitsu_os}_${_kitsu_arch}.tar.gz
_kitsu_base=${KITSU_DOWNLOAD_BASE:-https://github.com/belevtsev/kitsu/releases/download/$_kitsu_version}
_kitsu_base=${_kitsu_base%/}
_kitsu_http=0

validate_download_base() {
	case "$_kitsu_base" in
		https://?*) ;;
		http://*)
			_kitsu_authority=${_kitsu_base#http://}
			_kitsu_authority=${_kitsu_authority%%/*}
			case "$_kitsu_authority" in
				localhost|127.0.0.1|'[::1]') ;;
				localhost:*|127.0.0.1:*|'[::1]':*)
					_kitsu_port=${_kitsu_authority##*:}
					case "$_kitsu_port" in
						''|*[!0-9]*) fail 'HTTP download URLs are allowed only on loopback hosts' ;;
					esac
					;;
				*) fail 'HTTP download URLs are allowed only on loopback hosts' ;;
			esac
			_kitsu_http=1
			;;
		*) fail 'KITSU_DOWNLOAD_BASE must use HTTPS (loopback HTTP is allowed for local assets)' ;;
	esac
	case "$_kitsu_base" in
		*'?'*|*'#'*) fail 'KITSU_DOWNLOAD_BASE must name an asset directory without a query or fragment' ;;
	esac
}

download() {
	if [ "$_kitsu_downloader" = curl ]; then
		_kitsu_protocols='=https'
		[ "$_kitsu_http" = 0 ] || _kitsu_protocols='=http,https'
		curl --fail --silent --show-error --location \
			--proto "$_kitsu_protocols" --proto-redir '=https' --tlsv1.2 \
			--connect-timeout 15 --max-time 300 --output "$2" "$1"
	elif [ "$_kitsu_http" = 1 ]; then
		# An HTTP fixture must not redirect to another host or protocol.
		wget --quiet --tries=1 --timeout=30 --max-redirect=0 --output-document="$2" "$1"
	else
		wget --quiet --tries=1 --timeout=30 --https-only --output-document="$2" "$1"
	fi
}

umask 077
_kitsu_work=$(mktemp -d "${TMPDIR:-/tmp}/kitsu-install.XXXXXX") ||
	fail 'cannot create a temporary directory'
_kitsu_work=$(cd "$_kitsu_work" && pwd -P) || fail 'cannot open the temporary directory'
printf 'Installing kitsu %s for %s/%s...\n' "$_kitsu_version" "$_kitsu_os" "$_kitsu_arch"
if [ -n "$_kitsu_archive_dir" ]; then
	cp "$_kitsu_archive_dir/SHA256SUMS" "$_kitsu_work/SHA256SUMS" ||
		fail 'the archive directory does not contain a readable SHA256SUMS'
	cp "$_kitsu_archive_dir/$_kitsu_archive" "$_kitsu_work/archive.tar.gz" ||
		fail "the archive directory does not contain a readable $_kitsu_archive"
else
	validate_download_base
	if command -v curl >/dev/null 2>&1; then
		_kitsu_downloader=curl
	elif command -v wget >/dev/null 2>&1; then
		_kitsu_downloader=wget
	else
		fail 'downloading releases requires curl or wget'
	fi
	download "$_kitsu_base/SHA256SUMS" "$_kitsu_work/SHA256SUMS" ||
		fail "cannot download SHA256SUMS for Kitsu $_kitsu_version; check that this release is published at $_kitsu_base"
	download "$_kitsu_base/$_kitsu_archive" "$_kitsu_work/archive.tar.gz" ||
		fail "cannot download $_kitsu_archive; check that this release has an archive for $_kitsu_os/$_kitsu_arch"
fi

_kitsu_expected=$(LC_ALL=C awk -v archive="$_kitsu_archive" '
	{
		sub(/\r$/, "", $0)
		name = $2
		sub(/^\*/, "", name)
		if (name == archive) {
			count++
			if (NF != 2 || length($1) != 64 || $1 ~ /[^0-9a-fA-F]/) bad = 1
			digest = tolower($1)
		}
	}
	END {
		if (count != 1 || bad) exit 1
		print digest
	}
' "$_kitsu_work/SHA256SUMS") ||
	fail "SHA256SUMS must list $_kitsu_archive exactly once with a 64-digit SHA-256 checksum"
if [ "$_kitsu_checksum" = sha256sum ]; then
	_kitsu_digest_line=$(sha256sum <"$_kitsu_work/archive.tar.gz") || fail 'cannot compute the archive checksum'
else
	_kitsu_digest_line=$(shasum -a 256 <"$_kitsu_work/archive.tar.gz") || fail 'cannot compute the archive checksum'
fi
_kitsu_actual=$(printf '%s\n' "$_kitsu_digest_line" | awk 'NR == 1 { print tolower($1); exit }')
[ "$_kitsu_actual" = "$_kitsu_expected" ] || fail "checksum mismatch for $_kitsu_archive"

# List before extracting: the selected member must be unique, regular and executable.
# Extracting only that member to stdout never creates archive-controlled paths.
tar -tzf "$_kitsu_work/archive.tar.gz" >"$_kitsu_work/members" 2>/dev/null ||
	fail 'the release archive cannot be read'
awk '$0 == "kitsu" { count++ } END { exit count != 1 }' "$_kitsu_work/members" ||
	fail 'the release archive must contain exactly one kitsu file at its root'
tar -tvzf "$_kitsu_work/archive.tar.gz" kitsu >"$_kitsu_work/member" 2>/dev/null ||
	fail 'the kitsu archive member cannot be inspected'
awk '
	{ count++; if (substr($0, 1, 1) != "-" || substr($0, 4, 1) != "x") bad = 1 }
	END { exit count != 1 || bad }
' "$_kitsu_work/member" ||
	fail 'the root kitsu archive member must be a regular executable file'
tar -xOzf "$_kitsu_work/archive.tar.gz" kitsu >"$_kitsu_work/kitsu" ||
	fail 'the kitsu archive member cannot be extracted'
[ -s "$_kitsu_work/kitsu" ] || fail 'the kitsu archive member is empty'
chmod 755 "$_kitsu_work/kitsu" || fail 'cannot make the verified binary executable'
_kitsu_version_output=$("$_kitsu_work/kitsu" version 2>/dev/null) ||
	fail 'the verified binary cannot run on this system'
_kitsu_first_line=$(printf '%s\n' "$_kitsu_version_output" | awk 'NR == 1 { print; exit }')
[ "$_kitsu_first_line" = "kitsu $_kitsu_version" ] ||
	fail "the verified binary does not report kitsu $_kitsu_version"

mkdir -p "$_kitsu_install_dir" || fail "cannot create install directory: $_kitsu_install_dir"
_kitsu_install_dir=$(cd "$_kitsu_install_dir" && pwd -P) || fail 'cannot open the install directory'
_kitsu_target=$_kitsu_install_dir/kitsu
if [ -L "$_kitsu_target" ] || { [ -e "$_kitsu_target" ] && [ ! -f "$_kitsu_target" ]; }; then
	fail "install target must be a regular file or absent: $_kitsu_target"
fi
_kitsu_stage=$(mktemp "$_kitsu_install_dir/.kitsu-install.XXXXXX") ||
	fail 'cannot stage the verified binary in the install directory'
cp "$_kitsu_work/kitsu" "$_kitsu_stage" || fail 'cannot stage the verified binary'
chmod 755 "$_kitsu_stage" || fail 'cannot set the installed binary permissions'
mv -f "$_kitsu_stage" "$_kitsu_target" || fail 'cannot replace the installed binary'
_kitsu_stage=

path_warning() {
	printf 'kitsu installer: PATH was not updated in %s; add %s to PATH manually.\n' "$1" "$_kitsu_install_dir" >&2
}

update_profile() {
	_kitsu_profile=$1
	# Replacing a linked dotfile would break a user's dotfile manager.
	if [ -L "$_kitsu_profile" ] || { [ -e "$_kitsu_profile" ] && [ ! -f "$_kitsu_profile" ]; }; then
		path_warning "$_kitsu_profile"
		return
	fi
	if [ -f "$_kitsu_profile" ] && grep -Fqx "$_kitsu_export_line" "$_kitsu_profile"; then
		return
	fi
	_kitsu_profile_stage=$(mktemp "$_kitsu_profile.kitsu.XXXXXX") || {
		path_warning "$_kitsu_profile"
		return
	}
	if [ -f "$_kitsu_profile" ] && ! cp -p "$_kitsu_profile" "$_kitsu_profile_stage"; then
		rm -f "$_kitsu_profile_stage" || :
		_kitsu_profile_stage=
		path_warning "$_kitsu_profile"
		return
	fi
	if ! printf '\n# Added by the kitsu installer\ncase ":$PATH:" in\n    *:%s:*) ;;\n%s\nesac\n' \
		"$_kitsu_quoted_dir" "$_kitsu_export_line" >>"$_kitsu_profile_stage" ||
		! mv -f "$_kitsu_profile_stage" "$_kitsu_profile"; then
		rm -f "$_kitsu_profile_stage" || :
		_kitsu_profile_stage=
		path_warning "$_kitsu_profile"
		return
	fi
	_kitsu_profile_stage=
	printf 'Updated PATH setup in %s.\n' "$_kitsu_profile"
}

configure_path() {
	if [ "$_kitsu_no_modify_path" = 1 ]; then
		printf 'PATH setup was skipped; add %s to PATH if needed.\n' "$_kitsu_install_dir"
		return
	fi
	if [ -z "${HOME:-}" ] || ! command -v grep >/dev/null 2>&1 || ! command -v sed >/dev/null 2>&1; then
		path_warning 'the active shell profile'
		return
	fi
	_kitsu_escaped_dir=$(printf '%s' "$_kitsu_install_dir" | sed "s/'/'\\\\''/g") || {
		path_warning 'the active shell profile'
		return
	}
	_kitsu_quoted_dir="'$_kitsu_escaped_dir'"
	_kitsu_export="export PATH=$_kitsu_quoted_dir:\"\$PATH\""
	_kitsu_export_line="    *) $_kitsu_export ;;"
	_kitsu_shell=${SHELL:-sh}
	_kitsu_shell=${_kitsu_shell##*/}
	case "$_kitsu_shell" in
		zsh)
			update_profile "${ZDOTDIR:-$HOME}/.zshrc"
			;;
		bash)
			update_profile "$HOME/.bashrc"
			_kitsu_login_profile=
			for _kitsu_candidate in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
				if [ -e "$_kitsu_candidate" ] || [ -L "$_kitsu_candidate" ]; then
					_kitsu_login_profile=$_kitsu_candidate
					break
				fi
			done
			if [ -n "$_kitsu_login_profile" ]; then
				update_profile "$_kitsu_login_profile"
			elif [ "$_kitsu_os" = darwin ]; then
				update_profile "$HOME/.bash_profile"
			fi
			;;
		sh|dash|ash|ksh)
			update_profile "$HOME/.profile"
			;;
		*)
			path_warning "$_kitsu_shell startup files"
			;;
	esac
}

printf 'Installed kitsu %s at %s.\n' "$_kitsu_version" "$_kitsu_target"
configure_path
printf 'Open a new terminal or restart your shell for PATH changes to take effect.\n'
printf 'Run "%s version" to check the installed version.\n' "$_kitsu_target"
