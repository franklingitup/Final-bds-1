# Helm

- `charts/service/` — reusable chart for a stateless control-plane service.
- `control-plane/` — umbrella chart deploying all 12 services (aliases of `service`).
- `agent/` — chart installed into customer clusters (the generated install command runs this).
- `values/` — per-environment overrides.

```bash
# Render the control plane for dev
helm dependency build helm/control-plane
helm template control-plane helm/control-plane -f helm/values/dev.yaml

# Install the agent into a customer cluster
helm upgrade --install bds-agent helm/agent \
  --set controlPlane.endpoint=https://api.platform.example.com \
  --set controlPlane.installToken=<one-time-token>
```
