# Terraform / OpenTofu

Reusable modules with a uniform variable contract so the Provisioning Service
renders them identically. Customers execute the generated command locally; the
platform never stores cloud root credentials. See `docs/07-cluster-engine-design.md`.

```text
modules/
  eks/             # AWS EKS
  gke/             # GCP GKE
  aks/             # Azure AKS
  networking/      # shared networking primitives
  agent-bootstrap/ # installs the bds-agent Helm chart into the new cluster
templates/         # tfvars + backend templates rendered by Provisioning
examples/          # reference invocations for validation
```

Validate a module:

```bash
tofu -chdir=terraform/modules/eks init -backend=false
tofu -chdir=terraform/modules/eks validate
```
