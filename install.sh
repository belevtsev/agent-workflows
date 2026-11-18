#!/bin/sh
# Install a verified release archive without a source checkout or Go.
trap 'printf "%s\n" "kitsu installer: incomplete script" >&2; exit 1' 0

# Parse the entire installer before doing any work, including when piped to sh.
{
trap - 0
set -eu

_kitsu_version=${KITSU_VERSION:-}
_kitsu_install_dir=${KITSU_INSTALL_DIR:-}
_kitsu_no_modify_path=${KITSU_NO_MODIFY_PATH:-0}
_kitsu_archive_dir=${KITSU_ARCHIVE_DIR:-}
_kitsu_agent_host=
_kitsu_work=
_kitsu_stage=
_kitsu_profile_stage=

usage() {
	printf '%s\n' \
		'Usage: install.sh [--version TAG] [--install-dir DIR] [--no-modify-path]' \
		'                  [--archive-dir DIR] [--host codex|claude]' \
		'' \
		'With --host, install the latest CLI and matching plugin. CLI-only downloads default to v0.0.2. The default install directory is $HOME/.local/bin.' \
		'Environment: KITSU_VERSION, KITSU_INSTALL_DIR, KITSU_NO_MODIFY_PATH=1,' \
		'             KITSU_ARCHIVE_DIR, KITSU_DOWNLOAD_BASE.' \
		'With an archive directory, the installer uses the single archive tag for this platform in SHA256SUMS unless a version is provided.'
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

validate_version() {
	case "$_kitsu_version" in
		''|[!a-zA-Z0-9]*|*[!a-zA-Z0-9._+-]*)
			argument_error 'version must be a release tag containing letters, numbers, dots, underscores, + or -'
			;;
	esac
}

quote_shell_argument() {
	printf "'"
	while [ "$1" != "${1#*\'}" ]; do
		printf '%s%s' "${1%%\'*}" "'\\''"
		set -- "${1#*\'}"
	done
	printf "%s'" "$1"
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
		--host)
			need_value "$@"
			_kitsu_agent_host=$2
			shift 2
			;;
		--host=*)
			_kitsu_agent_host=${1#*=}
			[ -n "$_kitsu_agent_host" ] || argument_error '--host requires a value'
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

case "$_kitsu_agent_host" in
	'') ;;
	codex|claude) ;;
	*) argument_error '--host must be codex or claude' ;;
esac
if [ -z "$_kitsu_archive_dir$_kitsu_agent_host" ]; then
	_kitsu_version=${_kitsu_version:-v0.0.2}
fi
[ -z "$_kitsu_version" ] || validate_version
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
_kitsu_archive_suffix=_${_kitsu_os}_${_kitsu_arch}.tar.gz
_kitsu_archive=
if [ -n "$_kitsu_version" ]; then
	_kitsu_archive=kitsu_${_kitsu_version}${_kitsu_archive_suffix}
fi

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

verify_checksum() {
	if [ "$_kitsu_checksum" = sha256sum ]; then
		_kitsu_digest_line=$(sha256sum <"$1") || fail "cannot compute the checksum of $3"
	else
		_kitsu_digest_line=$(shasum -a 256 <"$1") || fail "cannot compute the checksum of $3"
	fi
	[ "${_kitsu_digest_line%% *}" = "$2" ] || fail "checksum mismatch for $3"
}

umask 077
_kitsu_work=$(mktemp -d "${TMPDIR:-/tmp}/kitsu-install.XXXXXX") ||
	fail 'cannot create a temporary directory'
_kitsu_work=$(cd "$_kitsu_work" && pwd -P) || fail 'cannot open the temporary directory'
_kitsu_remote_host=
_kitsu_official_latest=
if [ -n "$_kitsu_archive_dir" ]; then
	case "$_kitsu_archive_dir" in
		/*) ;;
		*) _kitsu_archive_dir=./$_kitsu_archive_dir ;;
	esac
	_kitsu_archive_dir=$(CDPATH= cd -P "$_kitsu_archive_dir" && printf '%s.' "$PWD") ||
		fail 'cannot open the archive directory'
	_kitsu_archive_dir=${_kitsu_archive_dir%.}
	cp "$_kitsu_archive_dir/SHA256SUMS" "$_kitsu_work/SHA256SUMS" ||
		fail 'the archive directory does not contain a readable SHA256SUMS'
else
	_kitsu_base=${KITSU_DOWNLOAD_BASE:-https://github.com/belevtsev/kitsu/releases/download/$_kitsu_version}
	_kitsu_remote_host=$_kitsu_agent_host
	if [ -z "${KITSU_DOWNLOAD_BASE:-}$_kitsu_version" ]; then
		_kitsu_base=https://github.com/belevtsev/kitsu/releases/latest/download
		_kitsu_official_latest=1
	fi
	_kitsu_base=${_kitsu_base%/}
	_kitsu_http=0
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
fi

if _kitsu_manifest_entry=$(LC_ALL=C awk -v archive="$_kitsu_archive" -v suffix="$_kitsu_archive_suffix" '
	{
		sub(/\r$/, "", $0)
		name = $2
		sub(/^\*/, "", name)
		if (archive != "") {
			if (name != archive) next
		} else if (length(name) < 6 + length(suffix) || substr(name, 1, 6) != "kitsu_" ||
			substr(name, length(name) - length(suffix) + 1) != suffix) {
			next
		}
		if (count && name != selected) ambiguous = 1
		selected = name
		count++
		if (NF != 2 || length($1) != 64 || $1 ~ /[^0-9a-fA-F]/) bad = 1
		digest = tolower($1)
	}
	END {
		if (ambiguous) exit 10
		if (!count && archive == "") exit 11
		if (count != 1 || bad) {
			print (count ? selected : archive)
			exit 12
		}
		print selected, digest
	}
' "$_kitsu_work/SHA256SUMS"); then
	_kitsu_archive=${_kitsu_manifest_entry% *}
	_kitsu_expected=${_kitsu_manifest_entry##* }
else
	case "$?" in
		10) fail "SHA256SUMS lists multiple Kitsu archives for $_kitsu_os/$_kitsu_arch. Specify --version TAG." ;;
		11) fail "SHA256SUMS does not list a Kitsu archive for $_kitsu_os/$_kitsu_arch" ;;
		12) fail "SHA256SUMS must list $_kitsu_manifest_entry exactly once with a 64-digit SHA-256 checksum" ;;
		*) fail 'cannot read SHA256SUMS' ;;
	esac
fi
if [ -z "$_kitsu_version" ]; then
	_kitsu_version=${_kitsu_archive#kitsu_}
	_kitsu_version=${_kitsu_version%"$_kitsu_archive_suffix"}
	validate_version
fi
if [ -n "$_kitsu_official_latest" ]; then
	_kitsu_base=https://github.com/belevtsev/kitsu/releases/download/$_kitsu_version
fi
printf 'Installing kitsu %s for %s/%s...\n' "$_kitsu_version" "$_kitsu_os" "$_kitsu_arch"
if [ -n "$_kitsu_archive_dir" ]; then
	cp "$_kitsu_archive_dir/$_kitsu_archive" "$_kitsu_work/archive.tar.gz" ||
		fail "the archive directory does not contain a readable $_kitsu_archive"
else
	download "$_kitsu_base/$_kitsu_archive" "$_kitsu_work/archive.tar.gz" ||
		fail "cannot download $_kitsu_archive; check that this release has an archive for $_kitsu_os/$_kitsu_arch"
fi
verify_checksum "$_kitsu_work/archive.tar.gz" "$_kitsu_expected" "$_kitsu_archive"
_kitsu_plugin_dir=$_kitsu_archive_dir
if [ -n "$_kitsu_remote_host" ]; then
	_kitsu_plugin_hash=$(awk '
		{ sub(/\r$/, ""); name=$2; sub(/^\*/, "", name) }
		name=="kitsu-plugin.zip" { count++; digest=$1; if (NF!=2) bad=1 }
		END { if (bad || count!=1 || length(digest)!=64 || digest ~ /[^0-9a-fA-F]/) exit 1; print tolower(digest) }
	' "$_kitsu_work/SHA256SUMS") || fail 'SHA256SUMS must list kitsu-plugin.zip exactly once with a valid checksum'
	_kitsu_plugin_dir=$_kitsu_work
	download "$_kitsu_base/kitsu-plugin.zip" "$_kitsu_plugin_dir/kitsu-plugin.zip" || fail 'cannot download kitsu-plugin.zip'
	verify_checksum "$_kitsu_plugin_dir/kitsu-plugin.zip" "$_kitsu_plugin_hash" kitsu-plugin.zip
fi

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
	if [ -z "${HOME:-}" ] || ! command -v grep >/dev/null 2>&1; then
		path_warning 'the active shell profile'
		return
	fi
	_kitsu_quoted_dir=$(quote_shell_argument "$_kitsu_install_dir")
	_kitsu_export="export PATH=$_kitsu_quoted_dir:\"\$PATH\""
	_kitsu_export_line="    *) $_kitsu_export ;;"
	case ":$PATH:" in
		*:"$_kitsu_install_dir":*) ;;
		*) printf 'Use kitsu in this shell now: %s\n' "$_kitsu_export" ;;
	esac
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
			else
				update_profile "$HOME/.profile"
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
if [ -n "$_kitsu_agent_host" ]; then
	"$_kitsu_target" agent setup --host "$_kitsu_agent_host" --archive-dir "$_kitsu_plugin_dir" || {
		[ -z "$_kitsu_remote_host" ] || fail "$_kitsu_agent_host setup failed. After fixing the reported problem, rerun the installer with --host $_kitsu_agent_host."
		fail "$_kitsu_agent_host setup failed. Retry with this command.
$(quote_shell_argument "$_kitsu_target") agent setup --host $_kitsu_agent_host --archive-dir $(quote_shell_argument "$_kitsu_archive_dir")"
	}
fi
printf 'Open a new terminal or restart your shell for PATH changes to take effect.\n'
printf 'Run "%s version" to check the installed version.\n' "$_kitsu_target"
}
