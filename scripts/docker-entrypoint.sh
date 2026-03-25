#!/bin/sh
set -e

DEFAULT_UID="${JAVINIZER_IMAGE_DEFAULT_UID:-1000}"
DEFAULT_GID="${JAVINIZER_IMAGE_DEFAULT_GID:-1000}"
container_uid="$(id -u)"
container_gid="$(id -g)"
requested_uid="${PUID:-${USER_ID:-${DEFAULT_UID}}}"
requested_gid="${PGID:-${GROUP_ID:-${DEFAULT_GID}}}"

is_numeric_id() {
    case "$1" in
        ''|*[!0-9]*)
            return 1
            ;;
        *)
            return 0
            ;;
    esac
}

ensure_group_entry() {
    target_gid="$1"

    if awk -F: -v gid="${target_gid}" '$3 == gid { found=1; exit } END { exit !found }' /etc/group; then
        return 0
    fi

    group_name="javinizer"
    if awk -F: -v name="${group_name}" '$1 == name { found=1; exit } END { exit !found }' /etc/group; then
        group_name="javinizer-${target_gid}"
    fi

    addgroup -g "${target_gid}" -S "${group_name}" >/dev/null
}

ensure_user_entry() {
    target_uid="$1"
    target_gid="$2"

    if awk -F: -v uid="${target_uid}" '$3 == uid { found=1; exit } END { exit !found }' /etc/passwd; then
        return 0
    fi

    group_name="$(awk -F: -v gid="${target_gid}" '$3 == gid { print $1; exit }' /etc/group)"
    if [ -z "${group_name}" ]; then
        echo "ERROR: No group entry exists for gid=${target_gid}."
        exit 1
    fi

    user_name="javinizer"
    if awk -F: -v name="${user_name}" '$1 == name { found=1; exit } END { exit !found }' /etc/passwd; then
        user_name="javinizer-${target_uid}"
    fi

    adduser -u "${target_uid}" -G "${group_name}" -s /bin/sh -D "${user_name}" >/dev/null
}

prepare_internal_path() {
    target_path="$1"
    target_uid="$2"
    target_gid="$3"

    mkdir -p "${target_path}"
    if ! awk -v path="${target_path}" '$2 == path { found=1; exit } END { exit found ? 0 : 1 }' /proc/mounts; then
        chown -R "${target_uid}:${target_gid}" "${target_path}"
    fi
}

if ! is_numeric_id "${requested_uid}"; then
    echo "ERROR: Invalid runtime UID '${requested_uid}'. Use PUID or USER_ID with a numeric value."
    exit 1
fi
if ! is_numeric_id "${requested_gid}"; then
    echo "ERROR: Invalid runtime GID '${requested_gid}'. Use PGID or GROUP_ID with a numeric value."
    exit 1
fi

if [ "${container_uid}" = "0" ] && [ "${JAVINIZER_RUNTIME_DROPPED:-0}" != "1" ]; then
    ensure_group_entry "${requested_gid}"
    ensure_user_entry "${requested_uid}" "${requested_gid}"
    prepare_internal_path /javinizer "${requested_uid}" "${requested_gid}"
    prepare_internal_path /media "${requested_uid}" "${requested_gid}"
    export JAVINIZER_RUNTIME_DROPPED=1
    exec su-exec "${requested_uid}:${requested_gid}" /usr/local/bin/docker-entrypoint.sh "$@"
fi

container_uid="$(id -u)"
container_gid="$(id -g)"

if [ -n "${requested_uid}" ] && [ "${requested_uid}" != "${container_uid}" ]; then
    echo "WARNING: Requested UID (${requested_uid}) does not match runtime UID (${container_uid})."
    echo "         Ensure container is started with matching user mapping."
fi
if [ -n "${requested_gid}" ] && [ "${requested_gid}" != "${container_gid}" ]; then
    echo "WARNING: Requested GID (${requested_gid}) does not match runtime GID (${container_gid})."
    echo "         Ensure container is started with matching user mapping."
fi

# Preflight write checks for mounted state directory.
if [ ! -d "/javinizer" ]; then
    echo "ERROR: /javinizer does not exist. Check your volume mapping."
    exit 1
fi

if ! mkdir -p /javinizer/logs /javinizer/cache; then
    echo "ERROR: Unable to create /javinizer/logs or /javinizer/cache."
    echo "       Container is running as uid=${container_uid} gid=${container_gid}."
    echo "       On Unraid, set PUID/PGID (or USER_ID/GROUP_ID) to match share ownership."
    exit 1
fi

javinizer_probe="/javinizer/.javinizer-write-test.$$"
if ! (umask 077 && : > "${javinizer_probe}") 2>/dev/null; then
    echo "ERROR: /javinizer is not writable by uid=${container_uid} gid=${container_gid}."
    echo "       Fix directory ownership/permissions or adjust PUID/PGID."
    exit 1
fi
rm -f "${javinizer_probe}" 2>/dev/null || true

# /media may be intentionally read-only for scan-only usage. Warn instead of failing.
if [ -d "/media" ]; then
    media_writable=0
    media_probe="/media/.javinizer-write-test.$$"
    if (umask 077 && : > "${media_probe}") 2>/dev/null; then
        rm -f "${media_probe}" 2>/dev/null || true
        media_writable=1
    else
        # Some environments keep /media root read-only while allowing writes
        # in specific subdirectories. Check existing children before warning.
        for media_dir in /media/*; do
            [ -d "${media_dir}" ] || continue
            media_probe="${media_dir}/.javinizer-write-test.$$"
            if (umask 077 && : > "${media_probe}") 2>/dev/null; then
                rm -f "${media_probe}" 2>/dev/null || true
                media_writable=1
                break
            fi
        done
    fi

    if [ "${media_writable}" -eq 0 ]; then
        echo "WARNING: /media is not writable by uid=${container_uid} gid=${container_gid}."
        echo "         Scan/review works, but organize/move/copy operations may fail."
    fi
fi

# Copy default config if it doesn't exist
if [ ! -f "/javinizer/config.yaml" ]; then
    echo "No config file found, copying default configuration..."
    cp /app/config/config.yaml.default /javinizer/config.yaml
    echo "✓ Default configuration created at /javinizer/config.yaml"
fi

# Execute the main command
exec "$@"
