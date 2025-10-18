#!/bin/bash

mkdir -p configs/networks

curl -s https://chainlist.org/rpcs.json | jq -c '.[] | select(.chainSlug != null and .rpc != null)' | while read -r obj; do
  slug=$(echo "$obj" | jq -r '.chainSlug' | tr '[:upper:]' '[:lower:]' | sed 's/ /-/g')
  chainId=$(echo "$obj" | jq -r '.chainId')

  echo "$obj" | jq --arg route "/$slug" --arg protocol "evm" --argjson chainId "$chainId" '
    {
      route: $route,
      protocol: $protocol,
      chainId: $chainId,
      timeoutMs: 1500,
      gas: {
        externalUrl: "",
        minPriorityGwei: 2,
        ttlSeconds: 10
      },
      nodes: (.rpc | map(select(.url | test("^https://")) | {url: .url, priority: 1}))
    }
  ' | yq -P > "configs/networks/${slug}.yaml"
done
