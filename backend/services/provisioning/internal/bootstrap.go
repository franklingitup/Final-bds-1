package provisioning

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// GenerateBootstrapToken generates a new random token and its hash.
// Used for session tokens and other authentication purposes.
func GenerateBootstrapToken() (token string, hash string, err error) {
	// Generate 32 random bytes
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}

	// Encode as base64
	token = base64.URLEncoding.EncodeToString(b)

	// Hash for storage
	h := sha256.Sum256([]byte(token))
	hash = hex.EncodeToString(h[:])

	return token, hash, nil
}

// HashToken hashes a token for storage/comparison.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Install step definitions
var DefaultInstallSteps = []StepInfo{
	{Number: 1, Name: "terraform_init", Description: "Initialize Terraform"},
	{Number: 2, Name: "terraform_plan", Description: "Plan infrastructure changes"},
	{Number: 3, Name: "terraform_apply", Description: "Apply infrastructure changes"},
	{Number: 4, Name: "wait_cluster", Description: "Wait for cluster to be ready"},
	{Number: 5, Name: "configure_kubectl", Description: "Configure kubectl access"},
	{Number: 6, Name: "install_agent", Description: "Install platform agent"},
	{Number: 7, Name: "agent_registration", Description: "Register agent with platform"},
	{Number: 8, Name: "verify_connection", Description: "Verify agent connection"},
}

// NOTE: The old BootstrapGenerator, BootstrapCommand, and agentManifestTemplate
// have been removed. Agents now use the cluster service's proven manifest at
// /v1/agent-install/:token/manifest.yaml, which generates the correct manifest
// with AGENT_TOKEN and CONTROL_PLANE_URL environment variables.
