// Package noblanketscopeauth provides a Go analysis pass that detects blanket
// authentication middleware patterns in Fiber route registration.
//
// This catches the anti-pattern where authentication middleware is applied to
// an empty or overly-broad path at the APPLICATION or VERSION-PREFIX level
// (e.g. app.Group("/v1", Auth) or v1.Group("", Auth)) which can inadvertently
// intercept public routes depending on registration order.
//
// It does NOT flag nested groups like `auth.Group("", RequireAuth)` where the
// parent is already a sub-path like /v1/auth — those are safe because they only
// affect routes under that sub-path.
//
// Usage: go vet -vettool=$(which noblanketscopeauth) ./...
package noblanketscopeauth

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "noblanketscopeauth",
	Doc:  "detects blanket auth middleware on empty or overly-broad route groups",
	Run:  run,
}

// Paths that indicate a blanket scope when used with auth middleware.
// Empty string "" is only flagged when on certain receiver names (see below).
var blanketVersionPaths = map[string]bool{
	`"/v1"`:  true,
	`"/v2"`:  true,
	`"/api"`: true,
}

// Receiver names that, when combined with .Group("", auth), indicate the
// dangerous blanket pattern. These are typically the version-level group
// variables assigned from app.Group("/v1").
var dangerousEmptyGroupReceivers = map[string]bool{
	"v1":  true,
	"v2":  true,
	"api": true,
	"app": true,
}

// Common middleware function/method names that indicate authentication.
var authMiddlewareNames = map[string]bool{
	"Auth":           true,
	"Authentication": true,
	"RequireAuth":    true,
	"Authenticate":   true,
	"AuthMiddleware": true,
	"JWTAuth":        true,
}

func run(pass *analysis.Pass) (interface{}, error) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			// Check if this is a .Group() call
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Group" {
				return true
			}

			// Must have at least 2 arguments: path and middleware
			if len(call.Args) < 2 {
				return true
			}

			// First argument should be a string literal (the path)
			pathArg, ok := call.Args[0].(*ast.BasicLit)
			if !ok || pathArg.Kind != token.STRING {
				return true
			}

			// Determine if this is a dangerous pattern
			isDangerous := false
			path := pathArg.Value

			if blanketVersionPaths[path] {
				// Patterns like app.Group("/v1", Auth) - always dangerous
				isDangerous = true
			} else if path == `""` || path == `"/"` {
				// Empty path is only dangerous on certain receivers
				// e.g., v1.Group("", Auth) is dangerous, but auth.Group("", Auth) is safe
				if ident, ok := sel.X.(*ast.Ident); ok {
					receiverName := strings.ToLower(ident.Name)
					isDangerous = dangerousEmptyGroupReceivers[receiverName]
				}
			}

			if !isDangerous {
				return true
			}

			// Check if any of the remaining arguments look like auth middleware
			for _, arg := range call.Args[1:] {
				if containsAuthMiddleware(arg) {
					pass.Reportf(call.Pos(),
						"blanket auth middleware on %s group can intercept public routes; "+
							"scope middleware to specific protected paths instead",
						path)
					break
				}
			}

			return true
		})
	}
	return nil, nil
}

// containsAuthMiddleware checks if an expression looks like an auth middleware.
func containsAuthMiddleware(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.CallExpr:
		// Check function calls like RequireAuth() or middleware.Auth()
		return containsAuthMiddleware(e.Fun)
	case *ast.SelectorExpr:
		// Check method/field access like middleware.Authentication
		return authMiddlewareNames[e.Sel.Name]
	case *ast.Ident:
		// Check plain identifiers
		return authMiddlewareNames[e.Name]
	}

	// Also check if the expression's string representation contains auth-related names
	if ident, ok := expr.(*ast.Ident); ok {
		name := strings.ToLower(ident.Name)
		return strings.Contains(name, "auth")
	}

	return false
}
