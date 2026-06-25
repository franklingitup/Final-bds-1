# Infra

- `local/` — configuration for the local dev stack (`docker-compose.yml`):
  - `prometheus/prometheus.yml`
  - `loki/loki-config.yml`
  - `grafana/provisioning/` (datasources wired to Prometheus + Loki)
- `gitops/` — ArgoCD/Flux application definitions (placeholder).
- `environments/` — per-environment infra config (dev/staging/prod) (placeholder).

The control-plane runtime manifests are delivered via the Helm charts in
`helm/`. This folder holds environment/platform infrastructure that is not
part of an individual service chart.
