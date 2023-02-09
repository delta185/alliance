#!/usr/bin/env bash

set -euo pipefail

# This script will join the testnet and start the node

declare GOA_VERSION="v0.0.1-goa"
declare readonly GITHUB_REPO="terra-money/alliance"
declare readonly GITHUB_URL="https://github.com/${GITHUB_REPO}"
declare readonly GITHUB_RAW="https://raw.githubusercontent.com/${GITHUB_REPO}/${GOA_VERSION}"

# Binaries don't exist so skipping this step
download_binary (){
    mkdir -p "${HOME}/bin"
    GOA_GZ="${GOA_VERSION}_$(uname -s  | tr '[:upper:]' '[:lower:]')_$(uname -m).tar.gz"
    GOA_DOWNLOAD="${GITHUB_URL}/releases/download/${GOA_VERSION}/${GOA_GZ}"
    echo "Downloading ${GOA_DOWNLOAD}"
    curl "${GOA_DOWNLOAD}" | tar -xz -C "${HOME}/bin"
}

verify_binary(){
    local binary=$1
    if [ ! -f "$binary" ]; then
        echo "Binary $binary does not exist"
        exit 1
    fi
    echo $binary
}

verify_chain_id (){
    local chain_id=$1
    case $chain_id in
        "atreides-1")
            echo $chain_id
        ;;
        "corrino-1")
            echo $chain_id
        ;;
        "harkonnen-1")
            echo $chain_id
        ;;
        "ordos-1")
            echo $chain_id
        ;;
        *)
            echo "Chain ID $chain_id is not supported"
            exit 1
        ;;
    esac
}

get_prefix(){
    local chain_id=$1
    cut -d "-" -f1 <<< $chain_id
}

get_denom(){
    local chain_id=$1
    echo "u$(cut -c-3  <<< $chain_id)"
}

get_peers(){
    for (( i=0; i<3; i++ )); do
        curl -sSL "https://${PREFIX}.terra.dev:26657/status" | \
        awk -vRS=',' -vFS='"' '/id":"/{print $4}; /listen_addr":"[0-9]/{print $4}' |\
        paste -sd "@" -
    done | paste -sd "," -
}

parse_options(){
    while [ $# -gt 0 ]; do
        case "$1" in
            -b|--binary)
                BINARY=$(verify_binary $2)
                shift 2
                ;;
            -c|--chain-id)
                CHAIN_ID=$(verify_chain_id $2)
                PREFIX=$(get_prefix $CHAIN_ID)
                DENOM=$(get_denom $CHAIN_ID)
                shift 2
                ;;
            -m|--moniker)
                MONIKER=$2
                shift 2
                ;;
            --)
                shift
                break
                ;;
            *)
                echo "Not implemented: $1" >&2
                exit 1
                ;;
        esac
    done
}

main(){
    parse_options $@
    rm -rf /Users/greg/.ordos
    
    echo "Initializing node"
    $BINARY init "${MONIKER}" --chain-id "${CHAIN_ID}" 2>&1 | sed -e 's/{.*}//' 

    echo "Downloading genesis file"
    curl -sSL "${GITHUB_RAW}/genesis/${CHAIN_ID}/genesis.json" -o "${HOME}/.${PREFIX}/config/genesis.json"

    echo "Getting peer list"
    PEERS="$(get_peers)"

    echo "Starting node"
    exec $BINARY start \
        --p2p.persistent_peers "$PEERS" 
}

main $@