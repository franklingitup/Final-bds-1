# Platform Agent

The Platform Agent runs inside customer Kubernetes clusters to register them with the control plane, report health/inventory, and reconcile desired deployment state.

## Features

- **Registration**: One-time registration using a token from the control plane
- **Heartbeat**: Periodic health check every 30 seconds
- **Inventory**: Reports Kubernetes version, node count, cloud provider, and region
- **Deployment Reconciliation**: Continuously reconciles desired deployment state into Kubernetes resources

## Configuration

### Core Settings

| Environment Variable | Required | Default | Description |
|---------------------|----------|---------|-------------|
| `AGENT_TOKEN` | Yes | - | Registration token from the control plane |
| `CONTROL_PLANE_URL` | Yes | - | Base URL of the control plane API |
| `AGENT_ID` | No | auto-generated | Override the generated agent ID |
| `HEARTBEAT_INTERVAL` | No | `30s` | How often to send heartbeats |
| `STATE_FILE` | No | `/var/lib/platform-agent/state.json` | Path to persist agent state |
| `DEBUG` | No | `false` | Enable debug logging |

### Reconciler Settings

| Environment Variable | Required | Default | Description |
|---------------------|----------|---------|-------------|
| `RECONCILER_ENABLED` | No | `false` | Enable deployment reconciliation |
| `ACCESS_TOKEN` | No* | - | JWT for Deployment Service API (*required if reconciler enabled) |
| `RECONCILE_INTERVAL` | No | `30s` | How often to poll for desired state |
| `RECONCILER_STATE_FILE` | No | `/var/lib/platform-agent/reconciler-state.json` | Path to persist reconciler state |
| `NAMESPACE` | No | `default` | Kubernetes namespace for workloads |

## Installation

### Using Helm (Basic)

```bash
helm install platform-agent ./helm/platform-agent \
  --namespace platform-system \
  --create-namespace \
  --set agent.token=<your-registration-token> \
  --set agent.controlPlaneUrl=https://api.platform.example.com
```

### Using Helm (With Reconciler)

```bash
helm install platform-agent ./helm/platform-agent \
  --namespace platform-system \
  --create-namespace \
  --set agent.token=<your-registration-token> \
  --set agent.controlPlaneUrl=https://api.platform.example.com \
  --set reconciler.enabled=true \
  --set reconciler.accessToken=<your-jwt-token> \
  --set reconciler.namespace=workloads
```

### Using an existing secret

```bash
# Create secret with token
kubectl create secret generic platform-agent-token \
  --namespace platform-system \
  --from-literal=token=<your-registration-token> \
  --from-literal=accessToken=<your-jwt-token>

# Install with existing secret
helm install platform-agent ./helm/platform-agent \
  --namespace platform-system \
  --set existingSecret=platform-agent-token \
  --set agent.controlPlaneUrl=https://api.platform.example.com \
  --set reconciler.enabled=true \
  --set reconciler.namespace=workloads
```

## Development

### Build

```bash
go build -o platform-agent ./cmd/agent
```

### Test

```bash
# Unit tests
go test ./...

# Integration tests (requires running control plane)
CONTROL_PLANE_URL=http://localhost:8085 \
AGENT_TOKEN=<token> \
go test -tags=integration ./internal/agent/...
```

### Docker

```bash
docker build -t platform-agent:latest .
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     Customer Cluster                             │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                      Platform Agent                          ││
│  │  ┌──────────────┐  ┌──────────────┐  ┌─────────────────┐   ││
│  │  │   Config     │  │  Inventory   │  │  Control Plane  │   ││
│  │  │   Loader     │  │  Collector   │  │     Client      │   ││
│  │  └──────────────┘  └──────────────┘  └─────────────────┘   ││
│  │         │                 │                   │             ││
│  │         └─────────────────┼───────────────────┘             ││
│  │                           │                                  ││
│  │  ┌──────────────┐  ┌──────────────┐  ┌─────────────────┐   ││
│  │  │    Agent     │  │  Reconciler  │  │   K8s Manager   │   ││
│  │  │   Runtime    │──│    Engine    │──│  (Deployments)  │   ││
│  │  └──────────────┘  └──────────────┘  └─────────────────┘   ││
│  └─────────────────────────────────────────────────────────────┘│
│                                                                  │
│  ┌────────────────────────────────────────┐                     │
│  │        Workload Namespace              │                     │
│  │  ┌────────────┐  ┌────────────┐       │                     │
│  │  │ Deployment │  │  Service   │  ...  │                     │
│  │  └────────────┘  └────────────┘       │                     │
│  └────────────────────────────────────────┘                     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ HTTPS
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Control Plane                             │
│  ┌───────────────────┐  ┌───────────────────┐                   │
│  │   Cluster Service │  │ Deployment Service│                   │
│  │  ┌─────────────┐  │  │  ┌─────────────┐  │                   │
│  │  │ Registration│  │  │  │  Desired    │  │                   │
│  │  │  Heartbeat  │  │  │  │   State     │  │                   │
│  │  └─────────────┘  │  │  │  Status API │  │                   │
│  └───────────────────┘  │  └─────────────┘  │                   │
│                         └───────────────────┘                   │
└─────────────────────────────────────────────────────────────────┘
```

## Lifecycle

1. **Startup**: Agent loads configuration and persisted state
2. **Registration**: If not registered, uses token to register with control plane
3. **Heartbeat Loop**: Sends periodic heartbeats with inventory updates
4. **Reconciliation Loop** (if enabled):
   - Polls Deployment Service for desired state every 30 seconds
   - Compares desired vs actual Kubernetes resources
   - Creates/updates Deployments and Services as needed
   - Reports status (started/succeeded/failed) back to control plane
   - Cleans up orphaned resources
5. **Shutdown**: Gracefully stops on SIGINT/SIGTERM

## State Persistence

The agent persists its state to avoid re-registration after restarts:

**Agent State** (`state.json`):
```json
{
  "agentId": "agent-abc12345",
  "clusterId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "organizationId": "01JZ3K4M5N6P7Q8R9S0T1U2V3X",
  "registered": true
}
```

**Reconciler State** (`reconciler-state.json`):
```json
{
  "appliedRevisions": {
    "dep-123": 5,
    "dep-456": 3
  },
  "reportedStatuses": {
    "rel-123": "succeeded",
    "rel-456": "succeeded"
  }
}
```

## RBAC Requirements

The agent requires the following Kubernetes permissions:

**For Inventory Collection:**
- `nodes`: list, get
- `namespaces`: get (for cluster UID from kube-system)

**For Deployment Reconciliation (if enabled):**
- `deployments`: create, get, list, update, delete (in workload namespace)
- `services`: create, get, list, update, delete (in workload namespace)

These are automatically created by the Helm chart.

## Reconciliation Behavior

The reconciler operates with the following guarantees:

1. **Idempotent**: Repeated reconciliation cycles do not create duplicates
2. **Drift Detection**: If Kubernetes resources differ from desired state, they are updated
3. **Orphan Cleanup**: Resources no longer in desired state are deleted
4. **Status Reporting**: Reports `started`, `succeeded`, `failed` status to control plane
5. **Revision Tracking**: Persists last applied revision to avoid redundant updates

## Future Enhancements (Not Implemented)

The following features are planned for future phases:

- Log collection
- Metrics collection
- Secret sync
- Domain/Ingress management
- Canary deployments
- Rollback automation
