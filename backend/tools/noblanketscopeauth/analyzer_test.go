package noblanketscopeauth_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/bdsplatform/platform/backend/tools/noblanketscopeauth"
)

func TestAnalyzer(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, noblanketscopeauth.Analyzer, "a")
}
