// Standalone, stdlib-only load generator for the Platform Agent control-plane
// endpoints (register + heartbeat). Kept in its own module with no external
// dependencies so it builds and runs anywhere without the platform's module
// graph.
module github.com/bdsplatform/platform/test/load

go 1.24
