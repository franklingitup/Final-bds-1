# proto

Source of truth for shared inter-service contracts and event payload schemas.
Code generation (Go types, TypeScript client types) is driven from here via
`scripts/codegen`. Event envelope and catalog are defined in
`docs/03-engineering-design.md` (Part 4).

```text
proto/
  events/     # event payload schemas
  services/   # gRPC service contracts (if/when adopted)
```
