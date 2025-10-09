<p align="center">
  <a href="./logo/mezzium-logo.png">
    <img src="./logo/mezzium-logo.png" alt="Mezzium logo" width="320">
  </a>
</p>

## Overview

**Mezzium** is a high‑reliability multi‑chain proxy for blockchain networks.  
It accepts incoming RPC, REST, and WebSocket requests and automatically forwards them to the best available upstream nodes. The system continuously monitors node health, balances traffic, and ensures fault tolerance.

Key capabilities include:

- **Load balancing and failover** – automatically selects healthy nodes with the lowest latency and removes failing ones.
- **Health checks and monitoring** – active validation of upstream nodes with detailed logging and Prometheus metrics.
- **Gas oracles** – built‑in computation of gas fees for Ethereum, Polygon, BSC, Arbitrum, Optimism, and other networks.
- **Multi‑chain support** – dozens of networks supported out of the box via YAML configuration, with the ability to add new ones dynamically through the Admin API.
- **Peer discovery and gossip** – nodes exchange information about available peers and discovered endpoints for horizontal scaling.
- **WebSocket proxying** – full support for subscription flows such as `eth_subscribe` and `eth_unsubscribe`.
- **Security** – sensitive values are redacted from logs, and administrative endpoints are protected with an API key.

Mezzium can be deployed as a unified access layer for applications that require fast, stable, and secure connectivity to multiple blockchain networks without the operational overhead of managing node infrastructure.

### Gossip & State Exchange
Mezzium nodes exchange not only peer lists but also **network state** (best and discovered nodes).  
This enables horizontal scaling and automatic discovery of new RPC endpoints across the cluster.

### Bootstrap
Nodes can join the cluster via a **bootstrap mechanism** using HMAC‑signed announce requests.  
This ensures authenticity of peers and prevents unauthorized injection of nodes.

### Admin API
Administrative endpoints allow dynamic management of networks and nodes:
- `POST /admin/networks` — add a new network
- `POST /admin/{network}/nodes` — add a node to a network
- `DELETE /admin/{network}/nodes` — remove a node
- `POST /admin/networks/bulk` — bulk add multiple networks

All admin endpoints are protected with `ADMIN_API_KEY`.

### Adapters & Shortcuts
Mezzium provides convenient REST shortcuts for common blockchain operations:
- `/eth/balance/{address}` → `eth_getBalance`
- `/btc/fees` → Bitcoin fee estimation via Tatum
- `/sol/slot` → Solana `getSlot`
- `/nft/{contract}/{tokenId}` → ERC‑721 `ownerOf`

These adapters simplify integration by exposing user‑friendly endpoints.

### Public API Endpoints
Predefined public endpoints are available out of the box:
- `/proxy/eth/fee`
- `/proxy/btc/fees`
- `/proxy/nft/get-all-nfts/{address}`
- `/proxy/nft/get-nft-metadata/{contract}/{tokenId}`
- `/proxy/eth/estimateGas`

### Leader Election & Heartbeat
A lightweight **leader election** mechanism is built in.  
Nodes elect a leader based on ID ordering, and the leader periodically sends heartbeats.  
Other nodes verify leader liveness within a configurable TTL.

### Discovered Nodes TTL
Nodes discovered through gossip are stored with a **time‑to‑live (TTL)**.  
Expired nodes are automatically pruned unless validated by health checks, ensuring the registry remains clean and reliable.

## Kubernetes Deployment

A minimal Kubernetes deployment example is provided in  
[`k8s/full-minimal-example.yaml`](k8s/full-minimal-example.yaml).

This manifest includes:
- **Deployment** with environment variables for Alchemy, Tatum, Admin API key, and bootstrap configuration.
- **Service** exposing the proxy on port 80.
- **Ingress** routing traffic from `mezzium.example.com`.
- **HorizontalPodAutoscaler** scaling pods between 1 and 5 replicas based on CPU utilization.

You can apply it directly:

```bash
kubectl apply -f k8s/full-minimal-example.yaml
```

##  Environment Variables

The Mezzium uses several environment variables to configure its behavior. Here's a complete list:

| Variable Name             | Description                                                    | Default Value       |
|---------------------------|----------------------------------------------------------------|---------------------|
| `SERVER_HOST`             | Host address to bind the HTTP server                           | `0.0.0.0`           |
| `SERVER_PORT`             | Port to bind the HTTP server                                   | `8080`              |
| `POD_IP`                  | Internal IP of the node (used for gossip/bootstrap)            | `127.0.0.1`         |
| `POD_NAME`                | Node name (used for gossip/bootstrap)                          | `dev-node`          |
| `SHARED_SECRET`           | Shared secret for bootstrap signature validation               | `devsecret`         |
| `BOOTSTRAP_URL`           | Optional URL of a bootstrap node                               | *(empty)*           |
| `TOR_SOCKS5`              | SOCKS5 proxy address for Tor-enabled nodes                     | `127.0.0.1:9050`    |
| `ADMIN_API_KEY`           | API key for accessing `/admin/*` endpoints                     | `changeme`          |
| `SWAGGER_HOST`            | Hostname for Swagger UI                                        | *(optional)*        |
| `TATUM_API_KEY`           | API key for Tatum RPC providers                                | *(required)*        |
| `TATUM_API_KEY_TESTNET`   | Optional testnet key for Tatum (now properly redacted in logs) | *(optional)*        |
| `ALCHEMY_API_KEY`         | API key for Alchemy RPC providers                              | *(required)*        |
| `ALCHEMY_API_KEY_TESTNET` | Optional testnet key for Alchemy RPC providers                 | *(optional)*        |

> ️ If `ADMIN_API_KEY` is left as `changeme`, admin endpoints are unprotected.

---

##  Secrets & Redaction

### Secret Injection from Environment

When loading network configurations from `configs/networks/*.yaml`, any value written as `${VAR_NAME}` will be automatically replaced at startup using `os.Getenv("VAR_NAME")`.

- If the environment variable is missing, a warning will be logged.
- The placeholder will be replaced with an empty string.

Example:

```yaml
headers:
  x-api-key: ${TATUM_API_KEY}
