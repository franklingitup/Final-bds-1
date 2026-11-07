// Command noblanketscopeauth is a Go vet tool that detects blanket auth
// middleware patterns in Fiber route registration.
//
// Install: go install ./backend/tools/noblanketscopeauth/cmd/noblanketscopeauth
// Usage:   go vet -vettool=$(which noblanketscopeauth) ./...
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/bdsplatform/platform/backend/tools/noblanketscopeauth"
)

func main() {
	singlechecker.Main(noblanketscopeauth.Analyzer)
}
