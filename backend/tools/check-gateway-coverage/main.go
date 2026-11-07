// check-gateway-coverage compares the number of HTTP route registrations in
// each backend service's RegisterRoutes function against the number of proxy
// registrations in the gateway's corresponding register*Routes function.
//
// It is a fast, zero-dependency source-level analysis tool: it uses regex to
// count method calls of the form .Get(/.Post(/.Patch(/.Delete(/.Put( rather
// than parsing ASTs, which keeps it simple and runnable without building any
// services.
//
// Usage:
//
//	go run ./tools/check-gateway-coverage          # from backend/ directory
//
// Exit code 1 if any service has more adjusted routes than the gateway proxies.
// "Adjusted" means the service's RegisterRoutes count minus the knownExclude
// value for routes that are intentionally not proxied through the user-facing
// gateway function (internal routes, health checks, routes handled by
// registerAgentRoutes, etc.).
//
// NOTE: This tool counts route registrations, not concrete paths. It catches
// missing proxy rules even when coverage_test.go in
// backend/services/gateway/internal/router/ has not yet been updated with the
// new concrete test paths. Treat a count mismatch as a prompt to update both
// router.go AND coverage_test.go.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// httpMethodRe matches lines like:   repos.Post("", ...)   or   app.Get("/foo", ...)
// Intentionally excludes .Group( calls (those create sub-routers, not routes).
var httpMethodRe = regexp.MustCompile(`\.(Get|Post|Patch|Delete|Put)\s*\(`)

// serviceEntry maps one backend service to the files and gateway function that
// cover its user-facing routes.
type serviceEntry struct {
	// name is used for reporting.
	name string

	// serviceFiles are the files that contain the service's RegisterRoutes
	// function (relative to the backend/ root).  Agent-only registration
	// functions (e.g. RegisterAgentRoutes) are typically in the same file
	// but are NOT a separate entry here — the tool only counts within the
	// function whose name is serviceFuncName.
	serviceFiles []string

	// serviceFuncName is the exact function name to extract from the service
	// files.  Defaults to "RegisterRoutes" when empty.
	serviceFuncName string

	// gatewayFunc is the name of the register*Routes function in router.go
	// that proxies this service's user-facing routes.
	gatewayFunc string

	// knownExclude is the number of HTTP method calls inside serviceFuncName
	// that are intentionally NOT proxied by gatewayFunc (e.g. internal-only
	// routes, health checks, or routes proxied by registerAgentRoutes).
	// Increment this when you intentionally skip a route; document why.
	knownExclude int

	// knownExcludeReason explains why knownExclude > 0, for the audit trail.
	knownExcludeReason string
}

// services is the canonical list of backend→gateway pairs.
// Keep this in sync with Config in backend/services/gateway/internal/router/router.go.
var services = []serviceEntry{
	{
		name:         "auth",
		serviceFiles: []string{"services/auth/internal/routes.go"},
		gatewayFunc:  "registerAuthRoutes",
		// 1 internal-only route: GET /internal/users (ListInternalUsers).
		// This route lives on /internal/* and is not proxied through the
		// public gateway — it is called service-to-service via the internal
		// network only.
		knownExclude:       1,
		knownExcludeReason: "GET /internal/users (ListInternalUsers) — internal service-to-service route, not gateway-proxied",
	},
	{
		name:         "tenant",
		serviceFiles: []string{"services/tenant/internal/routes.go"},
		gatewayFunc:  "registerTenantRoutes",
	},
	{
		name:         "project",
		serviceFiles: []string{"services/project/internal/routes.go"},
		gatewayFunc:  "registerProjectRoutes",
	},
	{
		name:         "audit",
		serviceFiles: []string{"services/audit/internal/routes.go"},
		gatewayFunc:  "registerAuditRoutes",
	},
	{
		name:         "cluster",
		serviceFiles: []string{"services/cluster/internal/routes.go"},
		gatewayFunc:  "registerClusterRoutes",
		// RegisterRoutes in the cluster service includes routes that the
		// gateway handles in separate functions:
		//   • 2 routes on /internal/* (CreateClusterWithToken, GetClusterInternal)
		//     — internal service-to-service only, not gateway-proxied.
		//   • 4 agent-capability routes (manifest.yaml, register ×2, recover)
		//     — proxied by registerAgentRoutes, not registerClusterRoutes.
		//     (/agent/register appears twice: once with rate-limiter, once without,
		//     but only one entry is actually registered at runtime via an if/else.)
		knownExclude:       6,
		knownExcludeReason: "2 /internal routes + 4 agent-capability routes (manifest.yaml, /agent/register ×2, /agent/recover) handled by registerAgentRoutes",
	},
	{
		name: "deployment",
		// Only RegisterRoutes is listed here.  RegisterAgentRoutes (3 routes)
		// is in the same file but handled by registerAgentRoutes in the gateway
		// and is therefore excluded from this comparison automatically because
		// countInFunction only counts within the named function body.
		serviceFiles:    []string{"services/deployment/internal/routes.go"},
		serviceFuncName: "RegisterRoutes",
		gatewayFunc:     "registerDeploymentRoutes",
	},
	{
		name:         "secrets",
		serviceFiles: []string{"services/secrets/internal/routes.go"},
		gatewayFunc:  "registerSecretsRoutes",
	},
	{
		name:         "domain",
		serviceFiles: []string{"services/domain/internal/routes.go"},
		gatewayFunc:  "registerDomainRoutes",
	},
	{
		name:         "notification",
		serviceFiles: []string{"services/notification/internal/routes.go"},
		gatewayFunc:  "registerNotificationRoutes",
	},
	{
		name:         "provisioning",
		serviceFiles: []string{"services/provisioning/internal/routes.go"},
		gatewayFunc:  "registerProvisioningRoutes",
	},
	{
		name: "build",
		// Build routes are split across the main service file and the
		// GitHub sub-package. Both have a RegisterRoutes function.
		serviceFiles: []string{
			"services/build/internal/routes.go",
			"services/build/internal/github/routes.go",
		},
		gatewayFunc: "registerBuildRoutes",
	},
	{
		name:         "observability",
		serviceFiles: []string{"services/observability/internal/routes.go"},
		gatewayFunc:  "registerObservabilityRoutes",
		// 1 health check: GET /health (HealthCheck).
		// This is a Kubernetes liveness probe endpoint on the service directly;
		// it is NOT proxied through the gateway (the gateway has its own health
		// endpoint). Agent ingest routes (POST /agent/metrics, /logs, /health)
		// ARE proxied via registerObservabilityRoutes (not registerAgentRoutes),
		// so they are included in both counts and cancel out.
		knownExclude:       1,
		knownExcludeReason: "GET /health (HealthCheck) — Kubernetes liveness probe on the service, not gateway-proxied",
	},
}

const gatewayRouterFile = "services/gateway/internal/router/router.go"

func main() {
	root, err := findBackendRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot locate backend/ root: %v\n", err)
		os.Exit(2)
	}

	gatewayLines, err := readLines(filepath.Join(root, gatewayRouterFile))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading gateway router: %v\n", err)
		os.Exit(2)
	}

	type result struct {
		entry        serviceEntry
		svcCount     int
		gwCount      int
		adjustedDiff int
	}

	var results []result
	anyMismatch := false

	for _, svc := range services {
		funcName := svc.serviceFuncName
		if funcName == "" {
			funcName = "RegisterRoutes"
		}

		svcCount := 0
		for _, f := range svc.serviceFiles {
			lines, err := readLines(filepath.Join(root, f))
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: cannot read %s: %v\n", f, err)
				continue
			}
			svcCount += countInFunction(lines, funcName, httpMethodRe)
		}

		gwCount := countInFunction(gatewayLines, svc.gatewayFunc, httpMethodRe)
		adjusted := svcCount - svc.knownExclude - gwCount

		results = append(results, result{
			entry:        svc,
			svcCount:     svcCount,
			gwCount:      gwCount,
			adjustedDiff: adjusted,
		})
		if adjusted > 0 {
			anyMismatch = true
		}
	}

	// Print table header.
	fmt.Printf("%-16s  %8s  %8s  %8s  %8s  %s\n",
		"SERVICE", "SVC_RTES", "EXCLUDED", "GW_RTES", "DIFF", "STATUS")
	fmt.Println(strings.Repeat("-", 72))

	for _, r := range results {
		status := "OK"
		if r.adjustedDiff > 0 {
			status = fmt.Sprintf("⚠  +%d route(s) in service have no proxy in %s",
				r.adjustedDiff, r.entry.gatewayFunc)
		} else if r.adjustedDiff < 0 {
			// Gateway proxies more than the service registers. This is usually OK:
			//   • gateway may merge routes from multiple service functions
			//   • service may have recently removed a route
			// Not necessarily a bug; reported as informational only.
			status = fmt.Sprintf("(gateway has %d extra; see comments in services entry)", -r.adjustedDiff)
		}
		diff := fmt.Sprintf("%+d", r.adjustedDiff)
		fmt.Printf("%-16s  %8d  %8d  %8d  %8s  %s\n",
			r.entry.name, r.svcCount, r.entry.knownExclude, r.gwCount, diff, status)
	}

	if anyMismatch {
		fmt.Println()
		fmt.Println("FAIL: one or more backend services have unproxied routes.")
		fmt.Println()
		for _, r := range results {
			if r.adjustedDiff > 0 {
				fmt.Printf("  %s: %d route(s) in %s not found in %s (router.go)\n",
					r.entry.name, r.adjustedDiff, funcName(r.entry), r.entry.gatewayFunc)
				if r.entry.knownExclude > 0 {
					fmt.Printf("    (already excluded: %d known non-proxied routes — %s)\n",
						r.entry.knownExclude, r.entry.knownExcludeReason)
				}
			}
		}
		fmt.Println()
		fmt.Println("Fix:")
		fmt.Println("  1. Add the missing proxy rule(s) to the appropriate register*Routes")
		fmt.Println("     function in backend/services/gateway/internal/router/router.go.")
		fmt.Println("  2. Add the concrete URL to TestGatewayCoverage*Routes in")
		fmt.Println("     backend/services/gateway/internal/router/coverage_test.go.")
		fmt.Println("  3. Re-run this tool to verify: go run ./tools/check-gateway-coverage/")
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("OK: all services are fully covered by gateway proxy rules.")
}

func funcName(e serviceEntry) string {
	if e.serviceFuncName != "" {
		return e.serviceFuncName
	}
	return "RegisterRoutes"
}

// countInFunction counts lines matching re inside the named function body.
// It uses brace-depth tracking to find the function boundaries.
func countInFunction(lines []string, targetFunc string, re *regexp.Regexp) int {
	inFunc := false
	depth := 0
	enteredBody := false
	count := 0

	for _, line := range lines {
		if !inFunc {
			// Match "func (r *Router) registerXxxRoutes(" or "func RegisterRoutes("
			if strings.Contains(line, "func ") && strings.Contains(line, targetFunc) {
				inFunc = true
			}
		}
		if !inFunc {
			continue
		}

		// Count brace depth to track function body boundaries.
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth > 0 {
			enteredBody = true
		}
		if re.MatchString(line) {
			count++
		}
		// Once we've entered the body (depth went positive) and it returns
		// to 0, the function has ended.
		if enteredBody && depth == 0 {
			break
		}
	}
	return count
}

// readLines reads a file and returns its lines.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// findBackendRoot walks up from the current directory looking for a directory
// that contains go.mod and a services/ subdirectory (the backend/ root).
func findBackendRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if fileExists(filepath.Join(dir, "go.mod")) && dirExists(filepath.Join(dir, "services")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not find backend/ root (directory with go.mod + services/)")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
