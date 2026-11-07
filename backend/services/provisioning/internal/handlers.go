package provisioning

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/bdsplatform/platform/backend/libs/database"
	apperrors "github.com/bdsplatform/platform/backend/libs/errors"
)

// Handler implements HTTP handlers for the provisioning service.
type Handler struct {
	svc *Service
}

// NewHandler creates a new handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Identity represents an authenticated user.
type Identity struct {
	UserID string
	Email  string
}

func extractIdentity(c *fiber.Ctx) (*Identity, error) {
	id, ok := c.Locals("identity").(*Identity)
	if !ok || id == nil {
		return nil, apperrors.Unauthenticated("authentication required")
	}
	return id, nil
}

func extractOrgID(c *fiber.Ctx) string {
	orgID := c.Params("orgId")
	if orgID == "" {
		orgID = c.Query("organizationId")
	}
	if orgID == "" {
		orgID = c.Get("X-Organization-ID")
	}
	return orgID
}

// ----------------------------------------------------------------------------
// Credentials
// ----------------------------------------------------------------------------

// CreateCredential handles POST /organizations/:orgId/credentials.
func (h *Handler) CreateCredential(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)

	var req CreateCredentialRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	cred, err := h.svc.CreateCredential(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToCredentialView(cred))
}

// ListCredentials handles GET /organizations/:orgId/credentials.
func (h *Handler) ListCredentials(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)

	creds, err := h.svc.ListCredentials(c.Context(), orgID, id.UserID)
	if err != nil {
		return err
	}

	views := make([]CredentialView, len(creds))
	for i, cr := range creds {
		views[i] = ToCredentialView(&cr)
	}

	return c.JSON(fiber.Map{"credentials": views})
}

// ValidateCredential handles POST /organizations/:orgId/credentials/:credentialId/validate.
// Returns 200 on successful validation, 422 Unprocessable Entity on validation failure
// (credential is syntactically valid but fails semantic validation against the cloud provider).
func (h *Handler) ValidateCredential(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	credentialID := c.Params("credentialId")

	cred, validationErr := h.svc.ValidateCredential(c.Context(), orgID, id.UserID, credentialID)
	if validationErr != nil {
		// Validation failed — return 422 with the credential view (includes validationError).
		// This distinguishes "credential exists but doesn't work" from "credential not found" (404)
		// or "internal error" (500).
		if cred != nil {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(ToCredentialView(cred))
		}
		return validationErr
	}

	return c.JSON(ToCredentialView(cred))
}

// DeleteCredential handles DELETE /organizations/:orgId/credentials/:credentialId.
func (h *Handler) DeleteCredential(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	credentialID := c.Params("credentialId")

	if err := h.svc.DeleteCredential(c.Context(), orgID, id.UserID, credentialID); err != nil {
		return err
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// ----------------------------------------------------------------------------
// Templates
// ----------------------------------------------------------------------------

// ListTemplates handles GET /organizations/:orgId/templates.
func (h *Handler) ListTemplates(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	provider := c.Query("provider")

	var providerPtr *string
	if provider != "" {
		providerPtr = &provider
	}

	templates, err := h.svc.ListTemplates(c.Context(), orgID, id.UserID, providerPtr)
	if err != nil {
		return err
	}

	views := make([]TemplateView, len(templates))
	for i, t := range templates {
		views[i] = ToTemplateView(&t)
	}

	return c.JSON(fiber.Map{"templates": views})
}

// ----------------------------------------------------------------------------
// Provisioning Requests
// ----------------------------------------------------------------------------

// CreateProvisioningRequest handles POST /organizations/:orgId/provisioning.
func (h *Handler) CreateProvisioningRequest(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)

	var req CreateProvisioningRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	provReq, err := h.svc.CreateProvisioningRequest(c.Context(), orgID, id.UserID, req)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToProvisioningRequestView(provReq))
}

// ListProvisioningRequests handles GET /organizations/:orgId/provisioning.
func (h *Handler) ListProvisioningRequests(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)

	page := database.PageRequest{
		Cursor: c.Query("cursor"),
		Limit:  c.QueryInt("limit", 20),
	}

	result, err := h.svc.ListProvisioningRequests(c.Context(), orgID, id.UserID, page)
	if err != nil {
		return err
	}

	views := make([]ProvisioningRequestView, len(result.Items))
	for i, r := range result.Items {
		views[i] = ToProvisioningRequestView(&r)
	}

	return c.JSON(fiber.Map{
		"requests":   views,
		"nextCursor": result.NextCursor,
	})
}

// GetProvisioningRequest handles GET /organizations/:orgId/provisioning/:requestId.
func (h *Handler) GetProvisioningRequest(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	requestID := c.Params("requestId")

	req, err := h.svc.GetProvisioningRequest(c.Context(), orgID, id.UserID, requestID)
	if err != nil {
		return err
	}

	return c.JSON(ToProvisioningRequestView(req))
}

// GenerateTerraform handles POST /organizations/:orgId/provisioning/:requestId/terraform.
func (h *Handler) GenerateTerraform(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	requestID := c.Params("requestId")

	req, err := h.svc.GenerateTerraform(c.Context(), orgID, id.UserID, requestID)
	if err != nil {
		return err
	}

	return c.JSON(ToProvisioningRequestView(req))
}

// StartProvisioning handles POST /organizations/:orgId/provisioning/:requestId/start.
func (h *Handler) StartProvisioning(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	requestID := c.Params("requestId")

	session, err := h.svc.StartProvisioning(c.Context(), orgID, id.UserID, requestID)
	if err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(ToInstallSessionView(session))
}

// ----------------------------------------------------------------------------
// Install Sessions
// ----------------------------------------------------------------------------

// GetInstallSession handles GET /organizations/:orgId/provisioning/:requestId/session.
func (h *Handler) GetInstallSession(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	sessionID := c.Params("sessionId")

	session, steps, err := h.svc.GetInstallSession(c.Context(), orgID, id.UserID, sessionID)
	if err != nil {
		return err
	}

	stepViews := make([]StepView, len(steps))
	for i, s := range steps {
		stepViews[i] = ToStepView(&s)
	}

	return c.JSON(fiber.Map{
		"session": ToInstallSessionView(session),
		"steps":   stepViews,
	})
}

// GetSessionBundle handles GET /sessions/:sessionToken/bundle.
func (h *Handler) GetSessionBundle(c *fiber.Ctx) error {
	bundle, err := h.svc.GetSessionBundle(c.Context(), c.Params("sessionToken"))
	if err != nil {
		return err
	}
	return c.JSON(bundle)
}

// UpdateStep handles POST /sessions/:sessionToken/steps/:stepNumber.
func (h *Handler) UpdateStep(c *fiber.Ctx) error {
	sessionToken := c.Params("sessionToken")
	stepNumber := c.Params("stepNumber")

	var stepNum int
	if _, err := fmt.Sscanf(stepNumber, "%d", &stepNum); err != nil {
		return apperrors.Validation("invalid step number")
	}

	var req UpdateStepRequest
	if err := c.BodyParser(&req); err != nil {
		return apperrors.Validation("invalid request body")
	}

	step, err := h.svc.UpdateStep(c.Context(), sessionToken, stepNum, req)
	if err != nil {
		return err
	}

	return c.JSON(ToStepView(step))
}

// ----------------------------------------------------------------------------
// Bootstrap
// ----------------------------------------------------------------------------

// NOTE: GetBootstrapManifest and ReportAgentConnection handlers removed.
// Agents now use the cluster service's /v1/agent-install/:token/manifest.yaml
// and /v1/agent/register endpoints directly.

// ----------------------------------------------------------------------------
// Install Script
// ----------------------------------------------------------------------------

// GetInstallScript handles GET /install/:sessionId.
// It serves a POSIX shell script that bootstraps cluster provisioning.
// The script reads PLATFORM_INSTALL_TOKEN from the environment for authentication.
func (h *Handler) GetInstallScript(c *fiber.Ctx) error {
	sessionID := c.Params("sessionId")

	// Validate session exists and is valid
	_, err := h.svc.ValidateSessionForInstall(c.Context(), sessionID)
	if err != nil {
		return err
	}

	// Derive the platform URL from the request
	scheme := "https"
	if c.Protocol() == "http" {
		scheme = "http"
	}
	platformURL := scheme + "://" + c.Hostname()

	// Generate the install script
	script := generateInstallScript(platformURL, sessionID)

	c.Set("Content-Type", "text/x-shellscript")
	c.Set("Cache-Control", "no-store")
	c.Set("Content-Disposition", "inline; filename=\"bds-install.sh\"")
	return c.SendString(script)
}

// generateInstallScript returns the POSIX shell script for cluster installation.
// The script requires PLATFORM_INSTALL_TOKEN to be set in the environment.
func generateInstallScript(platformURL, sessionID string) string {
	return `#!/bin/sh
# BDS Platform Cluster Installer
# Generated for session: ` + sessionID + `
# Platform: ` + platformURL + `
#
# Usage:
#   curl -sSL ` + platformURL + `/v1/install/` + sessionID + ` | PLATFORM_INSTALL_TOKEN=<token> sh
#
# This script performs the following steps:
#   1. Validates prerequisites (curl, jq, terraform/tofu, kubectl, cloud CLI)
#   2. Fetches the installer bundle from the platform
#   3. Writes Terraform configuration files
#   4. Runs terraform init and terraform apply
#   5. Configures kubectl access to the new cluster
#   6. Installs the BDS Platform agent
#   7. Waits for agent registration

set -e

# --- Configuration ---
PLATFORM_URL="` + platformURL + `"
SESSION_ID="` + sessionID + `"
WORK_DIR=".bds-install/${SESSION_ID}"
POLL_INTERVAL=10
POLL_TIMEOUT=600

# --- Color output (if terminal supports it) ---
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

log_info() {
    printf "${BLUE}[INFO]${NC} %s\n" "$1"
}

log_success() {
    printf "${GREEN}[OK]${NC} %s\n" "$1"
}

log_warn() {
    printf "${YELLOW}[WARN]${NC} %s\n" "$1"
}

log_error() {
    printf "${RED}[ERROR]${NC} %s\n" "$1" >&2
}

die() {
    log_error "$1"
    exit 1
}

# --- Cleanup on exit ---
cleanup() {
    if [ -d "${WORK_DIR}" ]; then
        log_info "Cleaning up working directory..."
        # Keep terraform state but remove sensitive files
        rm -f "${WORK_DIR}/terraform.tfvars.json" 2>/dev/null || true
        rm -f "${WORK_DIR}/gcp-service-account.json" 2>/dev/null || true
    fi
}
trap cleanup EXIT

# --- Step reporting ---
report_step() {
    step_num=$1
    status=$2
    output=$3
    error=$4

    json_body="{\"status\":\"${status}\""
    if [ -n "${output}" ]; then
        # Escape JSON special characters in output
        escaped_output=$(printf '%s' "${output}" | sed 's/\\/\\\\/g; s/"/\\"/g; s/	/\\t/g' | tr '\n' ' ' | head -c 65000)
        json_body="${json_body},\"output\":\"${escaped_output}\""
    fi
    if [ -n "${error}" ]; then
        escaped_error=$(printf '%s' "${error}" | sed 's/\\/\\\\/g; s/"/\\"/g; s/	/\\t/g' | tr '\n' ' ')
        json_body="${json_body},\"error\":\"${escaped_error}\""
    fi
    json_body="${json_body}}"

    curl -sS -X POST \
        -H "Content-Type: application/json" \
        -d "${json_body}" \
        "${PLATFORM_URL}/v1/sessions/${PLATFORM_INSTALL_TOKEN}/steps/${step_num}" >/dev/null 2>&1 || true
}

# --- Prerequisites check ---
check_prerequisites() {
    log_info "Checking prerequisites..."
    missing=""

    # Check for required tools
    if ! command -v curl >/dev/null 2>&1; then
        missing="${missing} curl"
    fi

    if ! command -v jq >/dev/null 2>&1; then
        missing="${missing} jq"
    fi

    # Check for terraform or tofu
    TERRAFORM_BIN=""
    if command -v tofu >/dev/null 2>&1; then
        TERRAFORM_BIN="tofu"
    elif command -v terraform >/dev/null 2>&1; then
        TERRAFORM_BIN="terraform"
    else
        missing="${missing} terraform/tofu"
    fi

    if ! command -v kubectl >/dev/null 2>&1; then
        missing="${missing} kubectl"
    fi

    if [ -n "${missing}" ]; then
        die "Missing required tools:${missing}"
    fi

    log_success "All prerequisites found (using ${TERRAFORM_BIN})"
}

# --- Validate token ---
validate_token() {
    if [ -z "${PLATFORM_INSTALL_TOKEN}" ]; then
        die "PLATFORM_INSTALL_TOKEN environment variable is required.

Usage:
  curl -sSL ${PLATFORM_URL}/v1/install/${SESSION_ID} | PLATFORM_INSTALL_TOKEN=<your-token> sh

Get your token from the BDS Platform console after creating a cluster provisioning request."
    fi
}

# --- Fetch bundle ---
fetch_bundle() {
    log_info "Fetching installer bundle from platform..."

    bundle_response=$(curl -sS -w "\n%{http_code}" \
        "${PLATFORM_URL}/v1/sessions/${PLATFORM_INSTALL_TOKEN}/bundle" 2>&1) || {
        die "Failed to connect to platform: ${bundle_response}"
    }

    http_code=$(echo "${bundle_response}" | tail -n1)
    bundle_json=$(echo "${bundle_response}" | sed '$d')

    if [ "${http_code}" != "200" ]; then
        error_msg=$(echo "${bundle_json}" | jq -r '.error.message // .error // "Unknown error"' 2>/dev/null || echo "${bundle_json}")
        die "Failed to fetch bundle (HTTP ${http_code}): ${error_msg}"
    fi

    # Extract fields from bundle
    PROVIDER=$(echo "${bundle_json}" | jq -r '.provider')
    TERRAFORM_CONFIG=$(echo "${bundle_json}" | jq -r '.terraformConfig')
    TERRAFORM_VARS=$(echo "${bundle_json}" | jq -c '.terraformVars')
    CREDENTIAL_ENV=$(echo "${bundle_json}" | jq -c '.credentialEnv // {}')
    BOOTSTRAP_TOKEN=$(echo "${bundle_json}" | jq -r '.bootstrapToken')
    SESSION_STATUS=$(echo "${bundle_json}" | jq -r '.status')

    if [ "${SESSION_STATUS}" = "completed" ]; then
        log_success "Cluster is already provisioned and ready!"
        exit 0
    fi

    if [ -z "${TERRAFORM_CONFIG}" ] || [ "${TERRAFORM_CONFIG}" = "null" ]; then
        die "Terraform configuration not ready. Please wait for terraform generation to complete."
    fi

    # Check for cloud CLI based on provider
    case "${PROVIDER}" in
        aws)
            if ! command -v aws >/dev/null 2>&1; then
                die "AWS CLI (aws) is required for AWS cluster provisioning"
            fi
            ;;
        gcp)
            if ! command -v gcloud >/dev/null 2>&1; then
                die "Google Cloud CLI (gcloud) is required for GCP cluster provisioning"
            fi
            ;;
        azure)
            if ! command -v az >/dev/null 2>&1; then
                die "Azure CLI (az) is required for Azure cluster provisioning"
            fi
            ;;
        *)
            die "Unsupported cloud provider: ${PROVIDER}"
            ;;
    esac

    log_success "Bundle fetched successfully (provider: ${PROVIDER})"
}

# --- Configure cloud credentials ---
configure_cloud_credentials() {
    log_info "Configuring cloud credentials..."

    case "${PROVIDER}" in
        aws)
            export AWS_ACCESS_KEY_ID=$(echo "${CREDENTIAL_ENV}" | jq -r '.AWS_ACCESS_KEY_ID // empty')
            export AWS_SECRET_ACCESS_KEY=$(echo "${CREDENTIAL_ENV}" | jq -r '.AWS_SECRET_ACCESS_KEY // empty')
            export AWS_SESSION_TOKEN=$(echo "${CREDENTIAL_ENV}" | jq -r '.AWS_SESSION_TOKEN // empty')
            ;;
        azure)
            export ARM_SUBSCRIPTION_ID=$(echo "${CREDENTIAL_ENV}" | jq -r '.ARM_SUBSCRIPTION_ID // empty')
            export ARM_TENANT_ID=$(echo "${CREDENTIAL_ENV}" | jq -r '.ARM_TENANT_ID // empty')
            export ARM_CLIENT_ID=$(echo "${CREDENTIAL_ENV}" | jq -r '.ARM_CLIENT_ID // empty')
            export ARM_CLIENT_SECRET=$(echo "${CREDENTIAL_ENV}" | jq -r '.ARM_CLIENT_SECRET // empty')
            az login --service-principal \
                --username "${ARM_CLIENT_ID}" \
                --password "${ARM_CLIENT_SECRET}" \
                --tenant "${ARM_TENANT_ID}" >/dev/null 2>&1 ||
                die "Failed to authenticate Azure CLI with the selected credential"
            az account set --subscription "${ARM_SUBSCRIPTION_ID}" ||
                die "Failed to select Azure subscription"
            ;;
        gcp)
            GOOGLE_PROJECT=$(echo "${CREDENTIAL_ENV}" | jq -r '.GOOGLE_PROJECT // empty')
            echo "${CREDENTIAL_ENV}" | jq -r '.GOOGLE_CREDENTIALS // empty' > "${WORK_DIR}/gcp-service-account.json"
            chmod 600 "${WORK_DIR}/gcp-service-account.json"
            export GOOGLE_APPLICATION_CREDENTIALS="${WORK_DIR}/gcp-service-account.json"
            export GOOGLE_PROJECT
            gcloud auth activate-service-account --key-file="${GOOGLE_APPLICATION_CREDENTIALS}" >/dev/null 2>&1 ||
                die "Failed to authenticate gcloud with the selected credential"
            gcloud config set project "${GOOGLE_PROJECT}" >/dev/null ||
                die "Failed to select GCP project"
            ;;
    esac

    log_success "Cloud credentials configured"
}

# --- Write terraform files ---
write_terraform_files() {
    log_info "Writing Terraform configuration..."

    mkdir -p "${WORK_DIR}"
    chmod 700 "${WORK_DIR}"
    WORK_DIR=$(cd "${WORK_DIR}" && pwd)

    # Write main.tf
    printf '%s\n' "${TERRAFORM_CONFIG}" > "${WORK_DIR}/main.tf"
    chmod 600 "${WORK_DIR}/main.tf"

    # Write terraform.tfvars.json
    printf '%s\n' "${TERRAFORM_VARS}" > "${WORK_DIR}/terraform.tfvars.json"
    chmod 600 "${WORK_DIR}/terraform.tfvars.json"

    # Write placeholder files
    echo "// declarations are included in main.tf" > "${WORK_DIR}/variables.tf"
    echo "// outputs are included in main.tf" > "${WORK_DIR}/outputs.tf"

    log_success "Terraform files written to ${WORK_DIR}"
}

# --- Run terraform ---
run_terraform() {
    cd "${WORK_DIR}"

    # Step 1: terraform init
    log_info "Running terraform init..."
    report_step 1 "running" "" ""
    
    init_output=$("${TERRAFORM_BIN}" init -input=false 2>&1) || {
        report_step 1 "failed" "${init_output}" "terraform init failed"
        die "Terraform init failed:\n${init_output}"
    }
    report_step 1 "completed" "${init_output}" ""
    log_success "Terraform initialized"

    # Step 2: terraform plan
    log_info "Running terraform plan..."
    report_step 2 "running" "" ""
    
    plan_output=$("${TERRAFORM_BIN}" plan -input=false -out=tfplan 2>&1) || {
        report_step 2 "failed" "${plan_output}" "terraform plan failed"
        die "Terraform plan failed:\n${plan_output}"
    }
    report_step 2 "completed" "${plan_output}" ""
    log_success "Terraform plan completed"

    # Step 3: terraform apply
    log_info "Running terraform apply (this may take 10-20 minutes)..."
    report_step 3 "running" "" ""
    
    apply_output=$("${TERRAFORM_BIN}" apply -input=false -auto-approve tfplan 2>&1) || {
        report_step 3 "failed" "${apply_output}" "terraform apply failed"
        die "Terraform apply failed:\n${apply_output}"
    }
    report_step 3 "completed" "${apply_output}" ""
    log_success "Terraform apply completed - infrastructure created"

    cd - >/dev/null
}

# --- Configure kubectl ---
configure_kubectl() {
    cd "${WORK_DIR}"

    # Step 4: Wait for cluster (get kubeconfig command)
    log_info "Retrieving cluster access configuration..."
    report_step 4 "running" "" ""
    
    kubeconfig_cmd=$("${TERRAFORM_BIN}" output -raw kubeconfig_command 2>&1) || {
        report_step 4 "failed" "" "Failed to get kubeconfig command from terraform output"
        die "Failed to get kubeconfig command"
    }
    report_step 4 "completed" "Retrieved kubeconfig command" ""

    # Step 5: Configure kubectl
    log_info "Configuring kubectl access..."
    report_step 5 "running" "" ""
    
    kubeconfig_output=$(sh -c "${kubeconfig_cmd}" 2>&1) || {
        report_step 5 "failed" "${kubeconfig_output}" "kubectl configuration failed"
        die "Failed to configure kubectl:\n${kubeconfig_output}"
    }
    report_step 5 "completed" "${kubeconfig_output}" ""
    log_success "kubectl configured"

    cd - >/dev/null
}

# --- Install agent ---
install_agent() {
    log_info "Fetching and installing BDS Platform agent..."
    report_step 6 "running" "" ""

    # Fetch agent manifest from cluster service using the real registration token
    manifest=$(curl -sS "${PLATFORM_URL}/v1/agent-install/${BOOTSTRAP_TOKEN}/manifest.yaml" 2>&1) || {
        report_step 6 "failed" "" "Failed to fetch agent manifest"
        die "Failed to fetch agent manifest"
    }

    # Apply manifest
    apply_output=$(echo "${manifest}" | kubectl apply -f - 2>&1) || {
        report_step 6 "failed" "${apply_output}" "Failed to apply agent manifest"
        die "Failed to apply agent manifest:\n${apply_output}"
    }
    report_step 6 "completed" "${apply_output}" ""
    log_success "BDS Platform agent installed"
}

# --- Wait for agent registration ---
wait_for_agent() {
    log_info "Waiting for agent to register with platform..."
    report_step 7 "running" "" ""

    elapsed=0
    while [ ${elapsed} -lt ${POLL_TIMEOUT} ]; do
        bundle_response=$(curl -sS "${PLATFORM_URL}/v1/sessions/${PLATFORM_INSTALL_TOKEN}/bundle" 2>/dev/null) || true
        agent_connected=$(echo "${bundle_response}" | jq -r '.agentConnected' 2>/dev/null || echo "false")
        agent_version=$(echo "${bundle_response}" | jq -r '.agentVersion // "unknown"' 2>/dev/null)

        if [ "${agent_connected}" = "true" ]; then
            report_step 7 "completed" "Agent connected (version: ${agent_version})" ""
            report_step 8 "running" "" ""
            report_step 8 "completed" "Connection verified" ""
            log_success "Agent registered successfully (version: ${agent_version})"
            return 0
        fi

        printf "."
        sleep ${POLL_INTERVAL}
        elapsed=$((elapsed + POLL_INTERVAL))
    done

    printf "\n"
    report_step 7 "failed" "" "Timed out waiting for agent registration"
    die "Timed out waiting for agent registration after ${POLL_TIMEOUT} seconds"
}

# --- Main ---
main() {
    printf "\n"
    printf "${BLUE}╔══════════════════════════════════════════════════════════════╗${NC}\n"
    printf "${BLUE}║       BDS Platform - Kubernetes Cluster Provisioning        ║${NC}\n"
    printf "${BLUE}╚══════════════════════════════════════════════════════════════╝${NC}\n"
    printf "\n"

    validate_token
    check_prerequisites
    fetch_bundle
    write_terraform_files
    configure_cloud_credentials
    run_terraform
    configure_kubectl
    install_agent
    wait_for_agent

    printf "\n"
    printf "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}\n"
    printf "${GREEN}║              Cluster provisioned successfully!              ║${NC}\n"
    printf "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}\n"
    printf "\n"
    printf "Your cluster is now ready and connected to the BDS Platform.\n"
    printf "View it in the console at: ${PLATFORM_URL}\n"
    printf "\n"
}

main
`
}

// ----------------------------------------------------------------------------
// Events
// ----------------------------------------------------------------------------

// ListEvents handles GET /organizations/:orgId/provisioning/:requestId/events.
func (h *Handler) ListEvents(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	requestID := c.Params("requestId")
	sessionID := c.Query("sessionId")

	page := database.PageRequest{
		Cursor: c.Query("cursor"),
		Limit:  c.QueryInt("limit", 50),
	}

	var reqPtr, sessPtr *string
	if requestID != "" {
		reqPtr = &requestID
	}
	if sessionID != "" {
		sessPtr = &sessionID
	}

	result, err := h.svc.ListEvents(c.Context(), orgID, id.UserID, reqPtr, sessPtr, page)
	if err != nil {
		return err
	}

	views := make([]EventView, len(result.Items))
	for i, e := range result.Items {
		views[i] = ToEventView(&e)
	}

	return c.JSON(fiber.Map{
		"events":     views,
		"nextCursor": result.NextCursor,
	})
}

// ----------------------------------------------------------------------------
// Provider Info
// ----------------------------------------------------------------------------

// ListRegions handles GET /providers/:provider/regions.
func (h *Handler) ListRegions(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	provider := c.Params("provider")
	credentialID := c.Query("credentialId")

	var credPtr *string
	if credentialID != "" {
		credPtr = &credentialID
	}

	regions, err := h.svc.ListRegions(c.Context(), orgID, id.UserID, provider, credPtr)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"regions": regions})
}

// ListMachineTypes handles GET /providers/:provider/machine-types.
func (h *Handler) ListMachineTypes(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	provider := c.Params("provider")
	region := c.Query("region", "us-east-1")

	types, err := h.svc.ListMachineTypes(c.Context(), orgID, id.UserID, provider, region)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"machineTypes": types})
}

// ListKubernetesVersions handles GET /providers/:provider/kubernetes-versions.
func (h *Handler) ListKubernetesVersions(c *fiber.Ctx) error {
	id, err := extractIdentity(c)
	if err != nil {
		return err
	}
	orgID := extractOrgID(c)
	provider := c.Params("provider")

	versions, err := h.svc.ListKubernetesVersions(c.Context(), orgID, id.UserID, provider)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"versions": versions})
}
