#!/usr/bin/env bash
set -euo pipefail

vault_bin=$(command -v vault)
probe_home=$(mktemp -d)
trap 'rm -rf "$probe_home"' EXIT

check_candidate() {
    local line=$1
    local expected=$2
    local output

    output=$(timeout 5s env -i \
        PATH="$PATH" \
        HOME="$probe_home" \
        VAULT_ADDR=not-a-url \
        COMP_LINE="$line" \
        COMP_POINT="${#line}" \
        "$vault_bin")
    if ! printf '%s\n' "$output" | rg -Fxq -- "$expected"; then
        printf 'missing %s for %s; got: %s\n' "$expected" "$line" "$output" >&2
        return 1
    fi
    printf '%s -> %s\n' "$line" "$expected"
}

check_candidate 'vault k' 'kv'
check_candidate 'vault kv g' 'get'
check_candidate 'vault kv get -m' '-mount'
check_candidate 'vault kv get -format=j' 'json'

if [ -n "$(find "$probe_home" \( -type f -o -type l \) -print -quit)" ]; then
    printf 'Vault wrote a file in the isolated home directory\n' >&2
    exit 1
fi

printf 'shell startup files: unchanged\n'
