# 20 — Secrets Service

This document describes the architecture, security model, and implementation of the Secrets Service for the BDS Kubernetes Application Platform.

---

## 1. Overview

The Secrets Service provides secure, project-scoped secret management. It enables customers to store sensitive configuration values (database URLs, API keys, credentials) that applications can consume at runtime via Kubernetes Secrets.

### 1.1 Key Design Principles

1. **Encryption at Rest**: All secrets are encrypted using AES-256-GCM envelope encryption before storage.
2. **Never Expose Plaintext**: Plaintext values are NEVER returned in API responses, events, or logs after creation.
3. **Project-Scoped**: Secrets are owned by projects and inherit project membership authorization.
4. **Agent-Only Decryption**: Only the Platform Agent can retrieve decrypted values via a dedicated secure endpoint.
5. **Transactional Events**: All state changes emit domain events through the transactional outbox.

### 1.2 Components

```
┌─────────────────────────────────────────────────────────────────┐
│                      API Gateway                                 │
├─────────────────────────────────────────────────────────────────┤
│  User API Routes                │  Agent API Routes              │
│  POST/GET/PATCH/DELETE          │  GET /agent/clusters/:id/secrets│
│  /organizations/:org/projects/  │  (X-Cluster-ID, X-Agent-ID)    │
│  :project/secrets/*             │                                │
└─────────────────────────────────────────────────────────────────┘
                      │                        │
                      ▼                        ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Secrets Service                              │
├─────────────────────────────────────────────────────────────────┤
│  ┌───────────┐  ┌────────────┐  ┌─────────────┐  ┌───────────┐  │
│  │ Handlers  │──│  Service   │──│  Repository │──│ PostgreSQL│  │
│  └───────────┘  └────────────┘  └─────────────┘  │   + RLS   │  │
│        │              │                          └───────────┘  │
│        │              │                                         │
│  ┌─────────────┐ ┌─────────────┐                               │
│  │ Token Auth  │ │ Agent Auth  │                               │
│  │ (JWT)       │ │ (Cluster)   │                               │
│  └─────────────┘ └─────────────┘                               │
│                       │                                         │
│              ┌────────────────┐                                 │
│              │   Encryptor    │                                 │
│              │  (AES-256-GCM) │                                 │
│              └────────────────┘                                 │
│                       │                                         │
│              ┌────────────────┐                                 │
│              │ Transactional  │──────────────────────► NATS    │
│              │    Outbox      │                                 │
│              └────────────────┘                                 │
└─────────────────────────────────────────────────────────────────┘
                                           │
                                           ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Platform Agent                               │
├─────────────────────────────────────────────────────────────────┤
│  ┌───────────────┐  ┌─────────────────┐  ┌──────────────────┐   │
│  │ Secrets Syncer│──│ K8s Secret Mgr  │──│ Kubernetes API   │   │
│  │ (60s poll)    │  │                 │  │                  │   │
│  └───────────────┘  └─────────────────┘  └──────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

---

## 2. Architecture

### 2.1 Database Schema

```sql
CREATE TABLE secrets (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID NOT NULL REFERENCES organizations(id),
    project_id       UUID NOT NULL REFERENCES projects(id),
    name             VARCHAR(255) NOT NULL,
    description      TEXT,
    encrypted_value  BYTEA NOT NULL,       -- AES-256-GCM encrypted
    value_hash       VARCHAR(128) NOT NULL, -- SHA-256 hash for change detection
    version          BIGINT NOT NULL DEFAULT 1,
    created_by       UUID,
    updated_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,           -- Soft delete
    
    CONSTRAINT secrets_project_name_unique 
        UNIQUE (project_id, name) WHERE deleted_at IS NULL
);

-- Row-Level Security
ALTER TABLE secrets ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON secrets
    USING (org_id = current_setting('app.current_org_id', true)::uuid);
```

### 2.2 Service Location

```
backend/services/secrets/
├── api/
│   └── openapi.yaml           # OpenAPI specification
├── cmd/server/
│   └── main.go                # Service entrypoint
└── internal/
    ├── agent_handlers.go      # Agent endpoint handlers
    ├── crypto.go              # AES-256-GCM encryption
    ├── crypto_test.go         # Encryption unit tests
    ├── domain.go              # Domain models and DTOs
    ├── errors.go              # Domain errors
    ├── events.go              # Event definitions
    ├── handlers.go            # User API handlers
    ├── middleware.go          # Authentication middleware
    ├── repository.go          # Database access
    ├── routes.go              # Route registration
    ├── service.go             # Business logic
    ├── service_test.go        # Service unit tests
    └── contract_test.go       # Event contract tests
```

---

## 3. Threat Model

### 3.1 Assets

| Asset | Sensitivity | Protection |
|-------|-------------|------------|
| Secret plaintext values | Critical | AES-256-GCM encryption, never in logs/events |
| Master encryption key | Critical | Environment variable, never stored in DB |
| Encrypted values | High | PostgreSQL with RLS, BYTEA column |
| Secret metadata | Medium | Project authorization, RLS |
| Value hash | Low | SHA-256, change detection only |

### 3.2 Threats and Mitigations

| Threat | Mitigation |
|--------|------------|
| Database breach | Encrypted at rest with AES-256-GCM |
| Log exposure | Plaintext never logged; SanitizedValue helper |
| Event leakage | Events contain only metadata, never values |
| Cross-tenant access | PostgreSQL RLS, explicit org_id checks |
| Agent impersonation | Cluster credential validation (X-Cluster-ID, X-Agent-ID) |
| Unauthorized access | Project membership + role-based permissions |
| Key compromise | Rotate SECRETS_MASTER_KEY, re-encrypt all secrets |
| Enumeration attack | Generic error messages |

### 3.3 Trust Boundaries

```
┌─────────────────────────────────────────────────────────────────┐
│                 Untrusted Zone (Internet)                        │
└─────────────────────────────────────────────────────────────────┘
                              │
                      ┌───────┴───────┐
                      │  API Gateway   │
                      │  (JWT/Cluster  │
                      │   validation)  │
                      └───────┬───────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│                   Trusted Zone (Internal)                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │   Secrets    │  │   Database   │  │ Platform     │          │
│  │   Service    │  │   (RLS)      │  │ Agent        │          │
│  └──────────────┘  └──────────────┘  └──────────────┘          │
└─────────────────────────────────────────────────────────────────┘
```

---

## 4. Encryption Model

### 4.1 Algorithm

- **Cipher**: AES-256-GCM (Galois/Counter Mode)
- **Key Size**: 256 bits (32 bytes)
- **Nonce Size**: 96 bits (12 bytes), randomly generated per encryption
- **Tag Size**: 128 bits (16 bytes), authentication tag

### 4.2 Ciphertext Format

```
┌────────────────────────────────────────────────────────┐
│ Nonce (12 bytes) │ Ciphertext (variable) │ Tag (16 bytes) │
└────────────────────────────────────────────────────────┘
```

### 4.3 Key Management

```bash
# Generate a new master key
SECRETS_MASTER_KEY=$(openssl rand -base64 32)

# Or use the helper function
go run ./tools/keygen
```

The master key is:
- Loaded from `SECRETS_MASTER_KEY` environment variable
- Never stored in the database
- Never logged
- Must be backed up securely (e.g., HashiCorp Vault, AWS KMS)

### 4.4 Key Rotation

To rotate the master key:

1. Generate a new key
2. Deploy new key alongside old key
3. Re-encrypt all secrets with new key (migration job)
4. Remove old key

```go
// Migration pseudo-code
func RotateKeys(oldKey, newKey *Encryptor) error {
    secrets, _ := repo.ListAll(ctx)
    for _, s := range secrets {
        plaintext, _ := oldKey.DecryptString(s.EncryptedValue)
        newCiphertext, _ := newKey.EncryptString(plaintext)
        s.EncryptedValue = newCiphertext
        repo.Update(ctx, s)
    }
}
```

---

## 5. Agent Sync Flow

### 5.1 Sequence Diagram

```
┌─────────┐          ┌─────────────┐          ┌─────────────┐
│ Platform│          │   Secrets   │          │ Kubernetes  │
│  Agent  │          │   Service   │          │   Cluster   │
└────┬────┘          └──────┬──────┘          └──────┬──────┘
     │                      │                        │
     │ GET /agent/clusters/ │                        │
     │ {clusterId}/secrets  │                        │
     │ X-Cluster-ID: ...    │                        │
     │ X-Agent-ID: ...      │                        │
     ├─────────────────────►│                        │
     │                      │                        │
     │                      │ Validate credentials   │
     │                      │ Set tenant context     │
     │                      │ Query secrets for      │
     │                      │ cluster's deployments  │
     │                      │ Decrypt values         │
     │                      │                        │
     │   200 OK             │                        │
     │   [{projectId,       │                        │
     │     name, value}]    │                        │
     │◄─────────────────────┤                        │
     │                      │                        │
     │                      │    Create/Update       │
     │                      │    K8s Secret          │
     │──────────────────────┼───────────────────────►│
     │                      │                        │
     │                      │    Secret Applied      │
     │◄─────────────────────┼────────────────────────┤
     │                      │                        │
```

### 5.2 Kubernetes Secret Format

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: bds-secret-{projectId}
  namespace: default
  labels:
    app.kubernetes.io/managed-by: bdsplatform-agent
    bdsplatform.io/project-id: project-123
  annotations:
    bdsplatform.io/version: "3"
    bdsplatform.io/synced-at: "2026-06-24T10:00:00Z"
type: Opaque
data:
  DATABASE_URL: cG9zdGdyZXM6Ly8uLi4=  # base64 encoded
  REDIS_URL: cmVkaXM6Ly8uLi4=
  STRIPE_API_KEY: c2tfbGl2ZV8uLi4=
```

### 5.3 Sync Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| Interval | 60s | Time between sync cycles |
| Namespace | default | K8s namespace for secrets |
| State File | /var/lib/platform-agent/secrets-state.json | Persistent state |

---

## 6. Event Flow

### 6.1 Events Produced

| Event | Description | Trigger |
|-------|-------------|---------|
| `secret.created.v1` | New secret created | POST /secrets |
| `secret.updated.v1` | Secret value/metadata changed | PATCH /secrets/:id |
| `secret.deleted.v1` | Secret soft-deleted | DELETE /secrets/:id |

### 6.2 Event Payload Contract

**CRITICAL**: Secret plaintext values are NEVER included in events.

```json
// secret.created.v1
{
  "secretId": "secret-123",
  "projectId": "project-456",
  "name": "DATABASE_URL",
  "version": 1
}

// secret.updated.v1
{
  "secretId": "secret-123",
  "projectId": "project-456",
  "name": "DATABASE_URL",
  "version": 2
}

// secret.deleted.v1
{
  "secretId": "secret-123",
  "projectId": "project-456",
  "name": "DATABASE_URL",
  "deletedBy": "user-789"
}
```

### 6.3 Event Consumers

| Consumer | Events | Purpose |
|----------|--------|---------|
| Cluster Agent | secret.* | Trigger secret sync |
| Audit Service | secret.* | Record audit trail |

---

## 7. API Reference

### 7.1 User Endpoints

| Method | Endpoint | Permission | Description |
|--------|----------|------------|-------------|
| POST | /v1/organizations/:orgId/projects/:projectId/secrets | secrets:write | Create secret |
| GET | /v1/organizations/:orgId/projects/:projectId/secrets | secrets:read | List secrets |
| GET | /v1/organizations/:orgId/projects/:projectId/secrets/:id | secrets:read | Get secret |
| PATCH | /v1/organizations/:orgId/projects/:projectId/secrets/:id | secrets:write | Update secret |
| DELETE | /v1/organizations/:orgId/projects/:projectId/secrets/:id | secrets:manage | Delete secret |

### 7.2 Agent Endpoint

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | /v1/agent/clusters/:clusterId/secrets | X-Cluster-ID, X-Agent-ID | Get decrypted secrets |

### 7.3 Authorization Matrix

| Role | Read Metadata | Create/Update | Delete |
|------|---------------|---------------|--------|
| Viewer | ✅ | ❌ | ❌ |
| Developer | ✅ | ✅ | ❌ |
| Admin | ✅ | ✅ | ✅ |

### 7.4 Request/Response Examples

**Create Secret**

```http
POST /v1/organizations/org-123/projects/proj-456/secrets
Authorization: Bearer <jwt>
Content-Type: application/json

{
  "name": "DATABASE_URL",
  "value": "postgres://user:pass@host:5432/db",
  "description": "Primary database connection"
}
```

```http
HTTP/1.1 201 Created

{
  "id": "secret-789",
  "projectId": "proj-456",
  "name": "DATABASE_URL",
  "description": "Primary database connection",
  "version": 1,
  "createdAt": "2026-06-24T10:00:00Z",
  "updatedAt": "2026-06-24T10:00:00Z"
}
```

**Note**: The `value` field only exists in the request. It is NEVER returned.

---

## 8. Deployment

### 8.1 Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| SECRETS_MASTER_KEY | Yes | Base64-encoded 32-byte AES key |
| DATABASE_URL | Yes | PostgreSQL connection string |
| NATS_URL | Yes | NATS JetStream URL |
| HTTP_PORT | No | Service port (default: 8087) |

### 8.2 Helm Values

```yaml
secrets:
  masterKey:
    existingSecret: secrets-master-key
    key: SECRETS_MASTER_KEY
  replicaCount: 2
  resources:
    requests:
      memory: "128Mi"
      cpu: "100m"
    limits:
      memory: "256Mi"
      cpu: "500m"
```

### 8.3 Health Checks

| Endpoint | Description |
|----------|-------------|
| GET /health | Service health |
| GET /ready | Readiness probe |

---

## 9. Success Criteria Validation

| Criterion | Status | Evidence |
|-----------|--------|----------|
| Create secret | ✅ | POST endpoint, service tests |
| Deploy application | ✅ | Deployment Service integration |
| Agent syncs secret | ✅ | Secrets syncer module |
| Kubernetes Secret created | ✅ | K8s Secret manager |
| Workload consumes secret | ✅ | Standard K8s secret mounting |
| Never in API responses | ✅ | SecretView excludes value |
| Never in events | ✅ | Contract tests verify |
| Never in logs | ✅ | No logging of plaintext |
| Never in audit | ✅ | Events contain metadata only |

---

## 10. References

- [OpenAPI Specification](../backend/services/secrets/api/openapi.yaml)
- [Event Catalog](./12-event-catalog.md#37-secrets-events)
- [Security Design](./06-security-design.md)
- [Architecture Overview](./02-architecture.md)
