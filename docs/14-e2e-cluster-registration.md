# 14 — End-to-End Test Plan: Cluster Registration Workflow

This document provides a complete end-to-end test plan for verifying the cluster registration workflow across all platform services.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Required Infrastructure](#2-required-infrastructure)
3. [Local Development Setup](#3-local-development-setup)
4. [API Calls](#4-api-calls)
5. [Database State Transitions](#5-database-state-transitions)
6. [Event Flow](#6-event-flow)
7. [Audit Records](#7-audit-records)
8. [Agent Behavior](#8-agent-behavior)
9. [Failure Scenarios](#9-failure-scenarios)
10. [Recovery Scenarios](#10-recovery-scenarios)
11. [Manual Validation Checklist](#11-manual-validation-checklist)

---

## 1. Overview

### 1.1 Workflow Summary

The cluster registration workflow enables customers to connect their existing Kubernetes clusters to the platform:

```
┌──────────────┐    ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│    User      │───▶│  API Gateway │───▶│   Cluster    │───▶│   Database   │
│  (Frontend)  │    │              │    │   Service    │    │  (Postgres)  │
└──────────────┘    └──────────────┘    └──────────────┘    └──────────────┘
                                               │
                                               ▼
                                        ┌──────────────┐
                                        │    NATS      │
                                        │  (Events)    │
                                        └──────────────┘
                                               │
                    ┌──────────────────────────┼──────────────────────────┐
                    ▼                          ▼                          ▼
             ┌──────────────┐           ┌──────────────┐           ┌──────────────┐
             │    Audit     │           │ Notification │           │    Other     │
             │   Service    │           │   Service    │           │  Consumers   │
             └──────────────┘           └──────────────┘           └──────────────┘

┌──────────────┐    ┌──────────────┐
│   Platform   │───▶│   Cluster    │  (Agent registers using token)
│    Agent     │    │   Service    │
└──────────────┘    └──────────────┘
```

### 1.2 Services Under Test

| Service | Port | Purpose |
|---------|------|---------|
| API Gateway | 8080 | Single entrypoint, auth, routing |
| Auth Service | 8081 | User authentication, JWT tokens |
| Tenant Service | 8082 | Organizations, memberships |
| Project Service | 8083 | Projects within organizations |
| Audit Service | 8084 | Event consumption, audit logging |
| Cluster Service | 8085 | Cluster registration, heartbeats |
| Platform Agent | N/A | Runs in customer cluster |

### 1.3 Actors

| Actor | Description |
|-------|-------------|
| **Platform User** | Human user interacting via frontend/API |
| **Platform Agent** | Software agent running in customer K8s cluster |
| **Background Job** | Scheduled tasks (disconnection detection) |

---

## 2. Required Infrastructure

### 2.1 Core Dependencies

| Component | Version | Purpose |
|-----------|---------|---------|
| PostgreSQL | 15+ | Primary database with RLS |
| NATS | 2.10+ | Event streaming with JetStream |
| Redis | 7+ | Session storage, rate limiting |

### 2.2 Database Requirements

```sql
-- Required extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Required function for RLS
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
```

### 2.3 NATS JetStream Configuration

```yaml
streams:
  - name: PLATFORM_EVENTS
    subjects:
      - "platform.>"
    retention: limits
    max_msgs: 1000000
    max_bytes: 1073741824  # 1GB
    max_age: 604800000000000  # 7 days in nanoseconds
    storage: file
    replicas: 1
    discard: old
```

### 2.4 Environment Variables

```bash
# Database
DATABASE_URL=postgres://platform:password@localhost:5432/platform?sslmode=disable

# NATS
NATS_URL=nats://localhost:4222
NATS_SUBJECT_PREFIX=platform

# Auth
JWT_SIGNING_KEY=your-256-bit-secret-key-for-jwt-signing

# Service URLs (for gateway)
AUTH_SERVICE_URL=http://localhost:8081
TENANT_SERVICE_URL=http://localhost:8082
PROJECT_SERVICE_URL=http://localhost:8083
AUDIT_SERVICE_URL=http://localhost:8084
CLUSTER_SERVICE_URL=http://localhost:8085
```

---

## 3. Local Development Setup

### 3.1 Start Infrastructure

```bash
# Start PostgreSQL
docker run -d --name platform-postgres \
  -e POSTGRES_USER=platform \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=platform \
  -p 5432:5432 \
  postgres:15-alpine

# Start NATS with JetStream
docker run -d --name platform-nats \
  -p 4222:4222 \
  -p 8222:8222 \
  nats:2.10-alpine -js

# Start Redis
docker run -d --name platform-redis \
  -p 6379:6379 \
  redis:7-alpine
```

### 3.2 Start Services

```bash
# Terminal 1: Auth Service
cd backend/services/auth
go run ./cmd/server

# Terminal 2: Tenant Service
cd backend/services/tenant
go run ./cmd/server

# Terminal 3: Project Service
cd backend/services/project
go run ./cmd/server

# Terminal 4: Audit Service
cd backend/services/audit
go run ./cmd/server

# Terminal 5: Cluster Service
cd backend/services/cluster
go run ./cmd/server

# Terminal 6: API Gateway
cd backend/services/gateway
go run ./cmd/server
```

### 3.3 Verify Services

```bash
# Check all services are healthy
curl http://localhost:8080/healthz

# Check individual services
curl http://localhost:8081/healthz  # Auth
curl http://localhost:8082/healthz  # Tenant
curl http://localhost:8083/healthz  # Project
curl http://localhost:8084/healthz  # Audit
curl http://localhost:8085/healthz  # Cluster
```

---

## 4. API Calls

### 4.1 Prerequisites: User and Organization Setup

#### Step 1: Create User Account

```bash
# POST /v1/auth/signup
curl -X POST http://localhost:8080/v1/auth/signup \
  -H "Content-Type: application/json" \
  -d '{
    "email": "admin@example.com",
    "password": "SecureP@ssw0rd!",
    "name": "Platform Admin"
  }'
```

**Expected Response (201 Created):**
```json
{
  "id": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "email": "admin@example.com",
  "name": "Platform Admin",
  "emailVerified": false,
  "createdAt": "2026-06-24T12:00:00Z"
}
```

#### Step 2: Login

```bash
# POST /v1/auth/login
curl -X POST http://localhost:8080/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "admin@example.com",
    "password": "SecureP@ssw0rd!"
  }'
```

**Expected Response (200 OK):**
```json
{
  "accessToken": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refreshToken": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expiresIn": 3600,
  "tokenType": "Bearer"
}
```

**Save the access token:**
```bash
export ACCESS_TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

#### Step 3: Create Organization

```bash
# POST /v1/organizations
curl -X POST http://localhost:8080/v1/organizations \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -d '{
    "name": "Acme Corporation",
    "slug": "acme-corp"
  }'
```

**Expected Response (201 Created):**
```json
{
  "id": "01JZ3K4M5N6P7Q8R9S0T1U2V3X",
  "name": "Acme Corporation",
  "slug": "acme-corp",
  "ownerUserId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
  "createdAt": "2026-06-24T12:01:00Z"
}
```

**Save the organization ID:**
```bash
export ORG_ID="01JZ3K4M5N6P7Q8R9S0T1U2V3X"
```

### 4.2 Cluster Registration Workflow

#### Step 4: Create Cluster

```bash
# POST /v1/organizations/{orgId}/clusters
curl -X POST "http://localhost:8080/v1/organizations/$ORG_ID/clusters" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -d '{
    "name": "Production Cluster",
    "slug": "production",
    "cloudProvider": "aws",
    "region": "us-west-2",
    "labels": {
      "environment": "production",
      "tier": "premium"
    }
  }'
```

**Expected Response (201 Created):**
```json
{
  "id": "01JZ3K4M5N6P7Q8R9S0T1U2V3Y",
  "organizationId": "01JZ3K4M5N6P7Q8R9S0T1U2V3X",
  "name": "Production Cluster",
  "slug": "production",
  "status": "pending",
  "cloudProvider": "aws",
  "region": "us-west-2",
  "labels": {
    "environment": "production",
    "tier": "premium"
  },
  "createdAt": "2026-06-24T12:02:00Z"
}
```

**Save the cluster ID:**
```bash
export CLUSTER_ID="01JZ3K4M5N6P7Q8R9S0T1U2V3Y"
```

**Validation Points:**
- [ ] Status is `pending`
- [ ] `agentId` is null
- [ ] `registeredAt` is null
- [ ] `lastHeartbeatAt` is null

#### Step 5: Generate Registration Token

```bash
# POST /v1/organizations/{orgId}/clusters/{clusterId}/tokens
curl -X POST "http://localhost:8080/v1/organizations/$ORG_ID/clusters/$CLUSTER_ID/tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -d '{
    "expiresIn": "24h"
  }'
```

**Expected Response (201 Created):**
```json
{
  "id": "01JZ3K4M5N6P7Q8R9S0T1U2V3Z",
  "clusterId": "01JZ3K4M5N6P7Q8R9S0T1U2V3Y",
  "status": "active",
  "expiresAt": "2026-06-25T12:02:00Z",
  "createdAt": "2026-06-24T12:02:00Z",
  "token": "dGVzdC1yZWdpc3RyYXRpb24tdG9rZW4..."
}
```

**Save the token:**
```bash
export REGISTRATION_TOKEN="dGVzdC1yZWdpc3RyYXRpb24tdG9rZW4..."
```

**Validation Points:**
- [ ] Token is returned only once (not retrievable again)
- [ ] Status is `active`
- [ ] `expiresAt` is 24 hours from now

#### Step 6: Agent Registration (Capability-Based)

```bash
# POST /v1/agent/register (No auth header - token is the credential)
curl -X POST http://localhost:8080/v1/agent/register \
  -H "Content-Type: application/json" \
  -d '{
    "token": "'$REGISTRATION_TOKEN'",
    "agentId": "agent-prod-001",
    "kubernetesVersion": "1.28.5",
    "nodeCount": 5,
    "cloudProvider": "aws",
    "region": "us-west-2"
  }'
```

**Expected Response (200 OK):**
```json
{
  "id": "01JZ3K4M5N6P7Q8R9S0T1U2V3Y",
  "organizationId": "01JZ3K4M5N6P7Q8R9S0T1U2V3X",
  "name": "Production Cluster",
  "slug": "production",
  "status": "connected",
  "kubernetesVersion": "1.28.5",
  "nodeCount": 5,
  "cloudProvider": "aws",
  "region": "us-west-2",
  "agentId": "agent-prod-001",
  "registeredAt": "2026-06-24T12:03:00Z",
  "lastHeartbeatAt": "2026-06-24T12:03:00Z",
  "createdAt": "2026-06-24T12:02:00Z"
}
```

**Validation Points:**
- [ ] Status changed to `connected`
- [ ] `agentId` is set
- [ ] `registeredAt` is set
- [ ] `lastHeartbeatAt` is set
- [ ] `kubernetesVersion` is updated
- [ ] `nodeCount` is updated

#### Step 7: Agent Heartbeat

```bash
# POST /v1/organizations/{orgId}/clusters/{clusterId}/heartbeat
curl -X POST "http://localhost:8080/v1/organizations/$ORG_ID/clusters/$CLUSTER_ID/heartbeat" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -d '{
    "agentId": "agent-prod-001",
    "kubernetesVersion": "1.28.6",
    "nodeCount": 6,
    "apiServerHealthy": true
  }'
```

**Expected Response (200 OK):**
```json
{
  "status": "ok"
}
```

**Validation Points:**
- [ ] `lastHeartbeatAt` updated
- [ ] `kubernetesVersion` updated if changed
- [ ] `nodeCount` updated if changed
- [ ] Heartbeat history recorded

### 4.3 Query Operations

#### Get Cluster Details

```bash
# GET /v1/organizations/{orgId}/clusters/{clusterId}
curl "http://localhost:8080/v1/organizations/$ORG_ID/clusters/$CLUSTER_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

#### List Clusters

```bash
# GET /v1/organizations/{orgId}/clusters
curl "http://localhost:8080/v1/organizations/$ORG_ID/clusters" \
  -H "Authorization: Bearer $ACCESS_TOKEN"

# Filter by status
curl "http://localhost:8080/v1/organizations/$ORG_ID/clusters?status=connected" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

#### Get Heartbeat History

```bash
# GET /v1/organizations/{orgId}/clusters/{clusterId}/heartbeats
curl "http://localhost:8080/v1/organizations/$ORG_ID/clusters/$CLUSTER_ID/heartbeats?limit=10" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

---

## 5. Database State Transitions

### 5.1 Clusters Table

```
State Diagram:
                    ┌─────────────────────────────────────────┐
                    │                                         │
                    ▼                                         │
┌─────────┐    ┌─────────────┐    ┌─────────────┐    ┌───────────────┐
│ pending │───▶│ registering │───▶│  connected  │───▶│ disconnected  │
└─────────┘    └─────────────┘    └─────────────┘    └───────────────┘
     │                                   │                    │
     │                                   │                    │
     └───────────────────────────────────┼────────────────────┘
                                         │
                                         ▼
                                   ┌─────────┐
                                   │ deleted │
                                   └─────────┘
```

### 5.2 State Verification Queries

```sql
-- After cluster creation (Step 4)
SELECT id, name, slug, status, agent_id, registered_at, last_heartbeat_at
FROM clusters WHERE id = '<cluster_id>';
-- Expected: status='pending', agent_id=NULL, registered_at=NULL

-- After token generation (Step 5)
SELECT id, cluster_id, status, expires_at, used_at
FROM cluster_registration_tokens WHERE cluster_id = '<cluster_id>';
-- Expected: status='active', used_at=NULL

-- After agent registration (Step 6)
SELECT id, name, status, agent_id, kubernetes_version, node_count, registered_at
FROM clusters WHERE id = '<cluster_id>';
-- Expected: status='connected', agent_id='agent-prod-001', kubernetes_version='1.28.5'

SELECT id, status, used_at, used_by_agent
FROM cluster_registration_tokens WHERE cluster_id = '<cluster_id>';
-- Expected: status='used', used_at NOT NULL, used_by_agent='agent-prod-001'

-- After heartbeat (Step 7)
SELECT id, kubernetes_version, node_count, last_heartbeat_at
FROM clusters WHERE id = '<cluster_id>';
-- Expected: kubernetes_version='1.28.6', node_count=6, last_heartbeat_at updated

SELECT COUNT(*) FROM cluster_heartbeats WHERE cluster_id = '<cluster_id>';
-- Expected: count > 0
```

### 5.3 RLS Verification

```sql
-- Test tenant isolation (should return 0 rows for different org)
SET app.current_org_id = '<different_org_id>';
SELECT * FROM clusters WHERE id = '<cluster_id>';
-- Expected: 0 rows (RLS blocks access)

-- Correct org (should return the cluster)
SET app.current_org_id = '<correct_org_id>';
SELECT * FROM clusters WHERE id = '<cluster_id>';
-- Expected: 1 row
```

---

## 6. Event Flow

### 6.1 Events Emitted

| Step | Event | Producer | Payload |
|------|-------|----------|---------|
| 4 | `cluster.created.v1` | Cluster Service | `{clusterId, name, slug, cloudProvider, region, createdBy}` |
| 5 | `cluster.registration.token.created.v1` | Cluster Service | `{clusterId, tokenId, expiresAt, deliveryRef}` |
| 6 | `cluster.registered.v1` | Cluster Service | `{clusterId, agentId, kubernetesVersion, nodeCount, cloudProvider, region}` |
| 7 | `cluster.heartbeat.received.v1` | Cluster Service | `{clusterId, agentId, kubernetesVersion, nodeCount, apiServerHealthy}` |

### 6.2 Event Verification via NATS

```bash
# Subscribe to cluster events
nats sub "platform.cluster.>"

# Expected output for each event:
# [#1] Received on "platform.cluster.created.v1"
# {
#   "eventId": "01JZ3K4M5N6P7Q8R9S0T1U2V3W",
#   "type": "cluster.created",
#   "version": 1,
#   "orgId": "01JZ3K4M5N6P7Q8R9S0T1U2V3X",
#   "occurredAt": "2026-06-24T12:02:00Z",
#   "actor": {"type": "user", "id": "01JZ3K4M5N6P7Q8R9S0T1U2V3W"},
#   "resource": {"type": "cluster", "id": "01JZ3K4M5N6P7Q8R9S0T1U2V3Y"},
#   "payload": {
#     "clusterId": "01JZ3K4M5N6P7Q8R9S0T1U2V3Y",
#     "name": "Production Cluster",
#     "slug": "production",
#     "cloudProvider": "aws",
#     "region": "us-west-2",
#     "createdBy": "01JZ3K4M5N6P7Q8R9S0T1U2V3W"
#   }
# }
```

### 6.3 Outbox Verification

```sql
-- Check transactional outbox for pending events
SELECT id, event_type, org_id, published_at, attempts
FROM outbox
WHERE event_type LIKE 'cluster.%'
ORDER BY created_at DESC
LIMIT 10;

-- Verify events are being relayed (published_at should be set)
SELECT COUNT(*) as pending FROM outbox WHERE published_at IS NULL;
-- Expected: 0 (relay is working)

SELECT COUNT(*) as published FROM outbox WHERE published_at IS NOT NULL;
-- Expected: > 0
```

---

## 7. Audit Records

### 7.1 Expected Audit Entries

| Event | Resource Type | Resource ID | Actor Type | Actor ID |
|-------|---------------|-------------|------------|----------|
| `cluster.created.v1` | cluster | cluster_id | user | user_id |
| `cluster.registration.token.created.v1` | cluster_registration_token | token_id | user | user_id |
| `cluster.registered.v1` | cluster | cluster_id | agent | agent_id |
| `cluster.heartbeat.received.v1` | cluster | cluster_id | agent | agent_id |

### 7.2 Query Audit Logs via API

```bash
# GET /v1/organizations/{orgId}/audit-logs
curl "http://localhost:8080/v1/organizations/$ORG_ID/audit-logs" \
  -H "Authorization: Bearer $ACCESS_TOKEN"

# Filter by resource
curl "http://localhost:8080/v1/organizations/$ORG_ID/audit-logs?resourceType=cluster&resourceId=$CLUSTER_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN"

# Filter by event type
curl "http://localhost:8080/v1/organizations/$ORG_ID/audit-logs?eventType=cluster.registered.v1" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

### 7.3 Database Verification

```sql
-- Verify audit records exist
SELECT 
    event_id,
    event_type,
    actor_type,
    actor_id,
    resource_type,
    resource_id,
    occurred_at
FROM audit_logs
WHERE organization_id = '<org_id>'
  AND resource_id = '<cluster_id>'
ORDER BY occurred_at;

-- Expected records:
-- 1. cluster.created.v1 (actor: user)
-- 2. cluster.registration.token.created.v1 (actor: user)
-- 3. cluster.registered.v1 (actor: agent)
-- 4. cluster.heartbeat.received.v1 (actor: agent) - multiple if heartbeats sent
```

---

## 8. Agent Behavior

### 8.1 Agent Startup Sequence

```
1. Load configuration from environment
   ├── AGENT_TOKEN (required)
   └── CONTROL_PLANE_URL (required)

2. Load persisted state (if exists)
   └── /var/lib/platform-agent/state.json

3. If not registered:
   ├── Collect cluster inventory
   │   ├── Kubernetes version
   │   ├── Node count
   │   ├── Cloud provider
   │   └── Region
   └── Call POST /v1/agent/register
       ├── Success: Save state, start heartbeat loop
       └── Failure: Retry with backoff

4. If already registered:
   └── Start heartbeat loop immediately

5. Heartbeat loop (every 30s):
   ├── Collect current inventory
   ├── Check API server health
   └── Call POST /v1/organizations/{orgId}/clusters/{clusterId}/heartbeat
```

### 8.2 Running the Agent Locally (Simulated)

```bash
# Set environment variables
export AGENT_TOKEN="$REGISTRATION_TOKEN"
export CONTROL_PLANE_URL="http://localhost:8080"
export STATE_FILE="/tmp/platform-agent/state.json"
export HEARTBEAT_INTERVAL="10s"  # Faster for testing
export DEBUG="true"

# Run the agent (simulated - uses fake inventory collector)
cd agents/platform-agent
go run ./cmd/agent
```

### 8.3 Agent State File

After successful registration:

```json
{
  "agentId": "agent-prod-001",
  "clusterId": "01JZ3K4M5N6P7Q8R9S0T1U2V3Y",
  "organizationId": "01JZ3K4M5N6P7Q8R9S0T1U2V3X",
  "registered": true
}
```

### 8.4 Agent Logs (Expected)

```
{"time":"2026-06-24T12:02:30Z","level":"INFO","msg":"platform agent starting"}
{"time":"2026-06-24T12:02:30Z","level":"INFO","msg":"generated agent ID","agent_id":"agent-prod-001"}
{"time":"2026-06-24T12:02:30Z","level":"INFO","msg":"starting registration","agent_id":"agent-prod-001"}
{"time":"2026-06-24T12:02:31Z","level":"INFO","msg":"registration successful","cluster_id":"01JZ3K4M5N6P7Q8R9S0T1U2V3Y","cluster_name":"Production Cluster","org_id":"01JZ3K4M5N6P7Q8R9S0T1U2V3X","status":"connected"}
{"time":"2026-06-24T12:02:31Z","level":"DEBUG","msg":"heartbeat sent","k8s_version":"1.28.5","node_count":5,"api_server_healthy":true}
{"time":"2026-06-24T12:03:01Z","level":"DEBUG","msg":"heartbeat sent","k8s_version":"1.28.5","node_count":5,"api_server_healthy":true}
```

---

## 9. Failure Scenarios

### 9.1 Invalid Registration Token

**Scenario:** Agent attempts registration with invalid/expired token.

**Test:**
```bash
curl -X POST http://localhost:8080/v1/agent/register \
  -H "Content-Type: application/json" \
  -d '{
    "token": "invalid-token",
    "agentId": "agent-001",
    "kubernetesVersion": "1.28.5",
    "nodeCount": 3
  }'
```

**Expected Response (401 Unauthorized):**
```json
{
  "error": "invalid or expired token"
}
```

**Agent Behavior:**
- Logs error and exits with fatal error
- Does NOT retry (token is permanently invalid)

### 9.2 Token Already Used

**Scenario:** Agent attempts registration with a token that was already used.

**Test:**
```bash
# Use the same token again after successful registration
curl -X POST http://localhost:8080/v1/agent/register \
  -H "Content-Type: application/json" \
  -d '{
    "token": "'$REGISTRATION_TOKEN'",
    "agentId": "agent-002",
    "kubernetesVersion": "1.28.5",
    "nodeCount": 3
  }'
```

**Expected Response (409 Conflict):**
```json
{
  "error": "registration token already used"
}
```

### 9.3 Token Revoked

**Scenario:** Admin revokes token before agent uses it.

**Test:**
```bash
# Revoke the token
curl -X DELETE "http://localhost:8080/v1/organizations/$ORG_ID/clusters/$CLUSTER_ID/tokens/$TOKEN_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN"

# Attempt registration
curl -X POST http://localhost:8080/v1/agent/register \
  -H "Content-Type: application/json" \
  -d '{
    "token": "'$REGISTRATION_TOKEN'",
    "agentId": "agent-001",
    "kubernetesVersion": "1.28.5",
    "nodeCount": 3
  }'
```

**Expected Response (401 Unauthorized):**
```json
{
  "error": "registration token revoked"
}
```

### 9.4 Agent ID Mismatch on Heartbeat

**Scenario:** Different agent attempts to send heartbeat.

**Test:**
```bash
curl -X POST "http://localhost:8080/v1/organizations/$ORG_ID/clusters/$CLUSTER_ID/heartbeat" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -d '{
    "agentId": "different-agent-id",
    "kubernetesVersion": "1.28.5",
    "nodeCount": 3,
    "apiServerHealthy": true
  }'
```

**Expected Response (403 Forbidden):**
```json
{
  "error": "agent ID mismatch"
}
```

### 9.5 Control Plane Unavailable

**Scenario:** Agent cannot reach control plane.

**Agent Behavior:**
- Registration: Retries with exponential backoff
- Heartbeat: Logs warning, continues trying next interval
- Does NOT crash or exit

**Logs:**
```
{"time":"...","level":"WARN","msg":"registration failed, retrying","error":"do request: dial tcp: connection refused"}
{"time":"...","level":"WARN","msg":"heartbeat failed","error":"do request: dial tcp: connection refused"}
```

### 9.6 Database Unavailable

**Scenario:** PostgreSQL is down during API call.

**Expected Response (503 Service Unavailable):**
```json
{
  "error": "service temporarily unavailable"
}
```

### 9.7 Cross-Tenant Access Attempt

**Scenario:** User tries to access cluster from different organization.

**Test:**
```bash
# Login as different user in different org
export OTHER_TOKEN="..."

# Attempt to access cluster
curl "http://localhost:8080/v1/organizations/$OTHER_ORG_ID/clusters/$CLUSTER_ID" \
  -H "Authorization: Bearer $OTHER_TOKEN"
```

**Expected Response (404 Not Found):**
```json
{
  "error": "cluster not found"
}
```

**Note:** RLS prevents the query from returning data, so it appears as "not found" rather than "forbidden" (no information leakage).

### 9.8 Rate Limiting

**Scenario:** Too many requests from same client.

**Test:**
```bash
# Send many requests quickly
for i in {1..100}; do
  curl -s -o /dev/null -w "%{http_code}\n" \
    "http://localhost:8080/v1/organizations/$ORG_ID/clusters" \
    -H "Authorization: Bearer $ACCESS_TOKEN"
done
```

**Expected:** After threshold, requests return 429 Too Many Requests.

---

## 10. Recovery Scenarios

### 10.1 Agent Restart After Registration

**Scenario:** Agent pod restarts after successful registration.

**Steps:**
1. Agent starts and loads state from `/var/lib/platform-agent/state.json`
2. Finds `registered: true` with cluster/org IDs
3. Skips registration, goes directly to heartbeat loop
4. First heartbeat marks cluster as `connected` (if it was `disconnected`)

**Verification:**
```bash
# Check agent logs show "already registered"
# Check cluster status returns to "connected"
```

### 10.2 Cluster Marked Disconnected, Agent Reconnects

**Scenario:** Agent was offline long enough to be marked disconnected.

**Steps:**
1. Stop agent for > 5 minutes
2. Background job marks cluster as `disconnected`
3. Restart agent
4. Agent sends heartbeat
5. Cluster status returns to `connected`

**Database State:**
```sql
-- Before reconnect
SELECT status FROM clusters WHERE id = '<cluster_id>';
-- Returns: disconnected

-- After reconnect (heartbeat received)
SELECT status FROM clusters WHERE id = '<cluster_id>';
-- Returns: connected
```

### 10.3 Token Expired, Generate New Token

**Scenario:** Registration token expired before agent could use it.

**Steps:**
1. Generate new token (revokes any existing active token)
2. Deploy agent with new token
3. Agent registers successfully

**Commands:**
```bash
# Generate new token
curl -X POST "http://localhost:8080/v1/organizations/$ORG_ID/clusters/$CLUSTER_ID/tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -d '{"expiresIn": "24h"}'
```

### 10.4 Agent State Lost, Re-Registration Needed

**Scenario:** PVC deleted or agent state file corrupted.

**Steps:**
1. Agent starts with empty state
2. Attempts registration with (used) token
3. Receives 409 Conflict
4. **Manual intervention required:** Generate new token and redeploy

**Resolution:**
```bash
# In control plane UI or API:
# 1. Delete the existing cluster
# 2. Create a new cluster
# 3. Generate new token
# 4. Redeploy agent with new token
```

### 10.5 Event Consumer Lag Recovery

**Scenario:** Audit service was down, missed events.

**Recovery:** NATS JetStream replays missed events.

**Verification:**
```bash
# Check consumer lag
nats consumer info PLATFORM_EVENTS audit-service

# Verify audit records eventually appear
SELECT COUNT(*) FROM audit_logs WHERE event_type LIKE 'cluster.%';
```

---

## 11. Manual Validation Checklist

### 11.1 Pre-Test Checklist

- [ ] PostgreSQL is running and accessible
- [ ] NATS JetStream is running and accessible
- [ ] All services are healthy (`/healthz` returns 200)
- [ ] Database migrations have been applied
- [ ] Test user and organization exist

### 11.2 Happy Path Checklist

| Step | Action | Expected Result | ✓ |
|------|--------|-----------------|---|
| 1 | Create cluster | Status=`pending`, 201 response | |
| 2 | Verify cluster in DB | Row exists with correct values | |
| 3 | Generate registration token | Token returned, status=`active` | |
| 4 | Verify token in DB | Row exists with hash, expires_at set | |
| 5 | Verify `cluster.created.v1` event | Event in NATS | |
| 6 | Verify `cluster.registration.token.created.v1` event | Event in NATS | |
| 7 | Start agent with token | Agent logs "starting registration" | |
| 8 | Verify agent registration | Cluster status=`connected`, 200 response | |
| 9 | Verify token marked used | Token status=`used`, used_at set | |
| 10 | Verify `cluster.registered.v1` event | Event in NATS | |
| 11 | Wait for heartbeat | Agent logs "heartbeat sent" | |
| 12 | Verify `cluster.heartbeat.received.v1` event | Event in NATS | |
| 13 | Query heartbeat history | Returns at least 1 heartbeat | |
| 14 | Query audit logs | All events recorded | |
| 15 | Stop agent | Agent logs "shutdown complete" | |

### 11.3 Error Handling Checklist

| Scenario | Expected Behavior | ✓ |
|----------|-------------------|---|
| Invalid token | 401 Unauthorized, agent exits | |
| Used token | 409 Conflict | |
| Revoked token | 401 Unauthorized | |
| Expired token | 401 Unauthorized | |
| Agent ID mismatch | 403 Forbidden | |
| Cross-tenant access | 404 Not Found (RLS) | |
| Control plane down | Agent retries/continues | |
| Rate limit exceeded | 429 Too Many Requests | |

### 11.4 Recovery Checklist

| Scenario | Expected Behavior | ✓ |
|----------|-------------------|---|
| Agent restart (registered) | Skips registration, heartbeats work | |
| Agent reconnects (disconnected) | Status returns to `connected` | |
| New token for expired | Old token revoked, new token works | |
| Event consumer catches up | All events eventually in audit | |

### 11.5 Performance Checklist

| Metric | Target | Actual | ✓ |
|--------|--------|--------|---|
| Cluster creation latency | < 500ms | | |
| Token generation latency | < 500ms | | |
| Agent registration latency | < 1000ms | | |
| Heartbeat latency | < 200ms | | |
| Event-to-audit latency | < 5s | | |

### 11.6 Security Checklist

| Check | Expected | ✓ |
|-------|----------|---|
| Token not in event payload | Only `deliveryRef` in event | |
| Token hash stored, not plaintext | DB has hash column | |
| RLS prevents cross-tenant access | Query returns empty | |
| JWT validation on protected routes | 401 without valid token | |
| Agent ID validated on heartbeat | 403 on mismatch | |
| Rate limiting active | 429 after threshold | |

---

## Appendix A: Quick Reference Commands

```bash
# Environment setup
export ACCESS_TOKEN="<jwt_token>"
export ORG_ID="<organization_id>"
export CLUSTER_ID="<cluster_id>"
export REGISTRATION_TOKEN="<registration_token>"

# Create cluster
curl -X POST "http://localhost:8080/v1/organizations/$ORG_ID/clusters" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -d '{"name": "Test Cluster", "slug": "test"}'

# Generate token
curl -X POST "http://localhost:8080/v1/organizations/$ORG_ID/clusters/$CLUSTER_ID/tokens" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -d '{"expiresIn": "24h"}'

# Register agent
curl -X POST http://localhost:8080/v1/agent/register \
  -H "Content-Type: application/json" \
  -d '{"token": "'$REGISTRATION_TOKEN'", "agentId": "agent-001", "kubernetesVersion": "1.28.5", "nodeCount": 3}'

# Send heartbeat
curl -X POST "http://localhost:8080/v1/organizations/$ORG_ID/clusters/$CLUSTER_ID/heartbeat" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -d '{"agentId": "agent-001", "kubernetesVersion": "1.28.5", "nodeCount": 3, "apiServerHealthy": true}'

# Query clusters
curl "http://localhost:8080/v1/organizations/$ORG_ID/clusters" \
  -H "Authorization: Bearer $ACCESS_TOKEN"

# Query audit logs
curl "http://localhost:8080/v1/organizations/$ORG_ID/audit-logs" \
  -H "Authorization: Bearer $ACCESS_TOKEN"
```

---

## Appendix B: Troubleshooting

### Agent Not Registering

1. Check token is correct and not expired
2. Check control plane URL is reachable from agent
3. Check agent logs for error messages
4. Verify cluster exists and is in `pending` status

### Events Not Appearing in Audit

1. Check NATS is running: `nats server check`
2. Check audit service is consuming: `nats consumer info PLATFORM_EVENTS audit-service`
3. Check outbox relay is running (events in outbox with `published_at = NULL`)
4. Check audit service logs for errors

### Cluster Stuck in "Pending"

1. Verify registration token was generated
2. Check agent is deployed with correct token
3. Check agent logs for registration errors
4. Check cluster service logs for API errors

### Heartbeats Not Updating

1. Verify agent ID matches registered agent
2. Check agent is sending heartbeats (logs)
3. Check cluster service logs for heartbeat errors
4. Verify cluster is in `connected` status

---

*Document Version: 1.0*
*Last Updated: 2026-06-24*
*Author: Principal Platform QA Engineer*
