package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SCIMServer implements SCIM 2.0 protocol for user and group provisioning.
type SCIMServer struct {
	users    SCIMUserStore
	groups   SCIMGroupStore
	baseURL  string
	bearerToken string
}

// SCIMUserStore persists SCIM users.
type SCIMUserStore interface {
	Create(ctx context.Context, user *SCIMUser) error
	Get(ctx context.Context, id string) (*SCIMUser, error)
	GetByUsername(ctx context.Context, username string) (*SCIMUser, error)
	GetByExternalID(ctx context.Context, externalID string) (*SCIMUser, error)
	List(ctx context.Context, filter string, startIndex, count int) (*SCIMListResponse[SCIMUser], error)
	Update(ctx context.Context, user *SCIMUser) error
	Delete(ctx context.Context, id string) error
}

// SCIMGroupStore persists SCIM groups.
type SCIMGroupStore interface {
	Create(ctx context.Context, group *SCIMGroup) error
	Get(ctx context.Context, id string) (*SCIMGroup, error)
	GetByDisplayName(ctx context.Context, displayName string) (*SCIMGroup, error)
	List(ctx context.Context, filter string, startIndex, count int) (*SCIMListResponse[SCIMGroup], error)
	Update(ctx context.Context, group *SCIMGroup) error
	Delete(ctx context.Context, id string) error
	AddMember(ctx context.Context, groupID, userID string) error
	RemoveMember(ctx context.Context, groupID, userID string) error
}

// SCIMUser represents a SCIM user resource.
type SCIMUser struct {
	Schemas     []string      `json:"schemas"`
	ID          string        `json:"id"`
	ExternalID  string        `json:"externalId,omitempty"`
	UserName    string        `json:"userName"`
	Name        *SCIMName     `json:"name,omitempty"`
	DisplayName string        `json:"displayName,omitempty"`
	NickName    string        `json:"nickName,omitempty"`
	ProfileURL  string        `json:"profileUrl,omitempty"`
	Title       string        `json:"title,omitempty"`
	UserType    string        `json:"userType,omitempty"`
	Emails      []SCIMEmail   `json:"emails,omitempty"`
	PhoneNumbers []SCIMPhone  `json:"phoneNumbers,omitempty"`
	Active      bool          `json:"active"`
	Groups      []SCIMGroupRef `json:"groups,omitempty"`
	Meta        *SCIMMeta     `json:"meta,omitempty"`
}

// SCIMName represents a user's name.
type SCIMName struct {
	Formatted       string `json:"formatted,omitempty"`
	FamilyName      string `json:"familyName,omitempty"`
	GivenName       string `json:"givenName,omitempty"`
	MiddleName      string `json:"middleName,omitempty"`
	HonorificPrefix string `json:"honorificPrefix,omitempty"`
	HonorificSuffix string `json:"honorificSuffix,omitempty"`
}

// SCIMEmail represents an email address.
type SCIMEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// SCIMPhone represents a phone number.
type SCIMPhone struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// SCIMGroupRef references a group.
type SCIMGroupRef struct {
	Value   string `json:"value"`
	Ref     string `json:"$ref,omitempty"`
	Display string `json:"display,omitempty"`
}

// SCIMGroup represents a SCIM group resource.
type SCIMGroup struct {
	Schemas     []string       `json:"schemas"`
	ID          string         `json:"id"`
	ExternalID  string         `json:"externalId,omitempty"`
	DisplayName string         `json:"displayName"`
	Members     []SCIMMember   `json:"members,omitempty"`
	Meta        *SCIMMeta      `json:"meta,omitempty"`
}

// SCIMMember represents a group member.
type SCIMMember struct {
	Value   string `json:"value"`
	Ref     string `json:"$ref,omitempty"`
	Display string `json:"display,omitempty"`
}

// SCIMMeta contains resource metadata.
type SCIMMeta struct {
	ResourceType string `json:"resourceType"`
	Created      string `json:"created,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	Location     string `json:"location,omitempty"`
	Version      string `json:"version,omitempty"`
}

// SCIMListResponse is the response for list operations.
type SCIMListResponse[T any] struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	ItemsPerPage int      `json:"itemsPerPage,omitempty"`
	StartIndex   int      `json:"startIndex,omitempty"`
	Resources    []T      `json:"Resources"`
}

// SCIMError represents a SCIM error response.
type SCIMError struct {
	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
	Detail   string   `json:"detail,omitempty"`
}

// SCIMPatchOp represents a PATCH operation.
type SCIMPatchOp struct {
	Schemas    []string         `json:"schemas"`
	Operations []SCIMOperation  `json:"Operations"`
}

// SCIMOperation is a single PATCH operation.
type SCIMOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path,omitempty"`
	Value interface{} `json:"value,omitempty"`
}

// SCIM schemas and constants.
const (
	SCIMSchemaUser       = "urn:ietf:params:scim:schemas:core:2.0:User"
	SCIMSchemaGroup      = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SCIMSchemaListResp   = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SCIMSchemaPatchOp    = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	SCIMSchemaError      = "urn:ietf:params:scim:api:messages:2.0:Error"
)

// NewSCIMServer creates a new SCIM server.
func NewSCIMServer(users SCIMUserStore, groups SCIMGroupStore, baseURL, bearerToken string) *SCIMServer {
	return &SCIMServer{
		users:       users,
		groups:      groups,
		baseURL:     strings.TrimSuffix(baseURL, "/"),
		bearerToken: bearerToken,
	}
}

// Authenticate validates the bearer token.
func (s *SCIMServer) Authenticate(r *http.Request) error {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return fmt.Errorf("missing authorization header")
	}

	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return fmt.Errorf("invalid authorization header")
	}

	if parts[1] != s.bearerToken {
		return fmt.Errorf("invalid token")
	}

	return nil
}

// HandleUsers handles /Users endpoints.
func (s *SCIMServer) HandleUsers(w http.ResponseWriter, r *http.Request) {
	if err := s.Authenticate(r); err != nil {
		s.writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	// Extract user ID from path
	path := strings.TrimPrefix(r.URL.Path, "/scim/v2/Users")
	userID := strings.TrimPrefix(path, "/")

	switch r.Method {
	case http.MethodGet:
		if userID == "" {
			s.listUsers(w, r)
		} else {
			s.getUser(w, r, userID)
		}
	case http.MethodPost:
		s.createUser(w, r)
	case http.MethodPut:
		s.replaceUser(w, r, userID)
	case http.MethodPatch:
		s.patchUser(w, r, userID)
	case http.MethodDelete:
		s.deleteUser(w, r, userID)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "invalidMethod", "method not allowed")
	}
}

// HandleGroups handles /Groups endpoints.
func (s *SCIMServer) HandleGroups(w http.ResponseWriter, r *http.Request) {
	if err := s.Authenticate(r); err != nil {
		s.writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/scim/v2/Groups")
	groupID := strings.TrimPrefix(path, "/")

	switch r.Method {
	case http.MethodGet:
		if groupID == "" {
			s.listGroups(w, r)
		} else {
			s.getGroup(w, r, groupID)
		}
	case http.MethodPost:
		s.createGroup(w, r)
	case http.MethodPut:
		s.replaceGroup(w, r, groupID)
	case http.MethodPatch:
		s.patchGroup(w, r, groupID)
	case http.MethodDelete:
		s.deleteGroup(w, r, groupID)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "invalidMethod", "method not allowed")
	}
}

func (s *SCIMServer) listUsers(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("filter")
	startIndex, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	if startIndex < 1 {
		startIndex = 1
	}
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count < 1 {
		count = 100
	}

	result, err := s.users.List(r.Context(), filter, startIndex, count)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "serverError", err.Error())
		return
	}

	result.Schemas = []string{SCIMSchemaListResp}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *SCIMServer) getUser(w http.ResponseWriter, r *http.Request, id string) {
	user, err := s.users.Get(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "notFound", "user not found")
		return
	}
	s.writeJSON(w, http.StatusOK, user)
}

func (s *SCIMServer) createUser(w http.ResponseWriter, r *http.Request) {
	var user SCIMUser
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalidSyntax", err.Error())
		return
	}

	user.Schemas = []string{SCIMSchemaUser}
	now := time.Now().UTC().Format(time.RFC3339)
	user.Meta = &SCIMMeta{
		ResourceType: "User",
		Created:      now,
		LastModified: now,
	}

	if err := s.users.Create(r.Context(), &user); err != nil {
		s.writeError(w, http.StatusConflict, "uniqueness", err.Error())
		return
	}

	user.Meta.Location = fmt.Sprintf("%s/scim/v2/Users/%s", s.baseURL, user.ID)
	s.writeJSON(w, http.StatusCreated, user)
}

func (s *SCIMServer) replaceUser(w http.ResponseWriter, r *http.Request, id string) {
	var user SCIMUser
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalidSyntax", err.Error())
		return
	}

	user.ID = id
	user.Schemas = []string{SCIMSchemaUser}
	user.Meta = &SCIMMeta{
		ResourceType: "User",
		LastModified: time.Now().UTC().Format(time.RFC3339),
		Location:     fmt.Sprintf("%s/scim/v2/Users/%s", s.baseURL, id),
	}

	if err := s.users.Update(r.Context(), &user); err != nil {
		s.writeError(w, http.StatusNotFound, "notFound", err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, user)
}

func (s *SCIMServer) patchUser(w http.ResponseWriter, r *http.Request, id string) {
	var patch SCIMPatchOp
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalidSyntax", err.Error())
		return
	}

	user, err := s.users.Get(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "notFound", "user not found")
		return
	}

	// Apply operations
	for _, op := range patch.Operations {
		if err := s.applyUserOp(user, op); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalidValue", err.Error())
			return
		}
	}

	user.Meta.LastModified = time.Now().UTC().Format(time.RFC3339)

	if err := s.users.Update(r.Context(), user); err != nil {
		s.writeError(w, http.StatusInternalServerError, "serverError", err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, user)
}

func (s *SCIMServer) applyUserOp(user *SCIMUser, op SCIMOperation) error {
	switch strings.ToLower(op.Op) {
	case "replace":
		return s.applyUserReplace(user, op.Path, op.Value)
	case "add":
		return s.applyUserAdd(user, op.Path, op.Value)
	case "remove":
		return s.applyUserRemove(user, op.Path)
	default:
		return fmt.Errorf("unsupported operation: %s", op.Op)
	}
}

func (s *SCIMServer) applyUserReplace(user *SCIMUser, path string, value interface{}) error {
	switch strings.ToLower(path) {
	case "active":
		if v, ok := value.(bool); ok {
			user.Active = v
		}
	case "username":
		if v, ok := value.(string); ok {
			user.UserName = v
		}
	case "displayname":
		if v, ok := value.(string); ok {
			user.DisplayName = v
		}
	case "name.givenname":
		if user.Name == nil {
			user.Name = &SCIMName{}
		}
		if v, ok := value.(string); ok {
			user.Name.GivenName = v
		}
	case "name.familyname":
		if user.Name == nil {
			user.Name = &SCIMName{}
		}
		if v, ok := value.(string); ok {
			user.Name.FamilyName = v
		}
	}
	return nil
}

func (s *SCIMServer) applyUserAdd(user *SCIMUser, path string, value interface{}) error {
	// Handle email additions, etc.
	return nil
}

func (s *SCIMServer) applyUserRemove(user *SCIMUser, path string) error {
	// Handle removals
	return nil
}

func (s *SCIMServer) deleteUser(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.users.Delete(r.Context(), id); err != nil {
		s.writeError(w, http.StatusNotFound, "notFound", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *SCIMServer) listGroups(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("filter")
	startIndex, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	if startIndex < 1 {
		startIndex = 1
	}
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if count < 1 {
		count = 100
	}

	result, err := s.groups.List(r.Context(), filter, startIndex, count)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "serverError", err.Error())
		return
	}

	result.Schemas = []string{SCIMSchemaListResp}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *SCIMServer) getGroup(w http.ResponseWriter, r *http.Request, id string) {
	group, err := s.groups.Get(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "notFound", "group not found")
		return
	}
	s.writeJSON(w, http.StatusOK, group)
}

func (s *SCIMServer) createGroup(w http.ResponseWriter, r *http.Request) {
	var group SCIMGroup
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalidSyntax", err.Error())
		return
	}

	group.Schemas = []string{SCIMSchemaGroup}
	now := time.Now().UTC().Format(time.RFC3339)
	group.Meta = &SCIMMeta{
		ResourceType: "Group",
		Created:      now,
		LastModified: now,
	}

	if err := s.groups.Create(r.Context(), &group); err != nil {
		s.writeError(w, http.StatusConflict, "uniqueness", err.Error())
		return
	}

	group.Meta.Location = fmt.Sprintf("%s/scim/v2/Groups/%s", s.baseURL, group.ID)
	s.writeJSON(w, http.StatusCreated, group)
}

func (s *SCIMServer) replaceGroup(w http.ResponseWriter, r *http.Request, id string) {
	var group SCIMGroup
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalidSyntax", err.Error())
		return
	}

	group.ID = id
	group.Schemas = []string{SCIMSchemaGroup}
	group.Meta = &SCIMMeta{
		ResourceType: "Group",
		LastModified: time.Now().UTC().Format(time.RFC3339),
		Location:     fmt.Sprintf("%s/scim/v2/Groups/%s", s.baseURL, id),
	}

	if err := s.groups.Update(r.Context(), &group); err != nil {
		s.writeError(w, http.StatusNotFound, "notFound", err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, group)
}

func (s *SCIMServer) patchGroup(w http.ResponseWriter, r *http.Request, id string) {
	var patch SCIMPatchOp
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalidSyntax", err.Error())
		return
	}

	group, err := s.groups.Get(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "notFound", "group not found")
		return
	}

	// Apply operations
	for _, op := range patch.Operations {
		if err := s.applyGroupOp(r.Context(), group, op); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalidValue", err.Error())
			return
		}
	}

	group.Meta.LastModified = time.Now().UTC().Format(time.RFC3339)

	if err := s.groups.Update(r.Context(), group); err != nil {
		s.writeError(w, http.StatusInternalServerError, "serverError", err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, group)
}

func (s *SCIMServer) applyGroupOp(ctx context.Context, group *SCIMGroup, op SCIMOperation) error {
	switch strings.ToLower(op.Op) {
	case "add":
		if strings.ToLower(op.Path) == "members" {
			return s.addGroupMembers(ctx, group, op.Value)
		}
	case "remove":
		if strings.HasPrefix(strings.ToLower(op.Path), "members") {
			return s.removeGroupMembers(ctx, group, op.Path)
		}
	case "replace":
		if op.Path == "displayName" {
			if v, ok := op.Value.(string); ok {
				group.DisplayName = v
			}
		}
	}
	return nil
}

func (s *SCIMServer) addGroupMembers(ctx context.Context, group *SCIMGroup, value interface{}) error {
	members, ok := value.([]interface{})
	if !ok {
		return fmt.Errorf("invalid members value")
	}

	for _, m := range members {
		member, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		userID, _ := member["value"].(string)
		if userID != "" {
			if err := s.groups.AddMember(ctx, group.ID, userID); err != nil {
				return err
			}
			group.Members = append(group.Members, SCIMMember{Value: userID})
		}
	}
	return nil
}

func (s *SCIMServer) removeGroupMembers(ctx context.Context, group *SCIMGroup, path string) error {
	// Parse path like "members[value eq \"user-id\"]"
	// Simplified implementation
	return nil
}

func (s *SCIMServer) deleteGroup(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.groups.Delete(r.Context(), id); err != nil {
		s.writeError(w, http.StatusNotFound, "notFound", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *SCIMServer) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *SCIMServer) writeError(w http.ResponseWriter, status int, scimType, detail string) {
	err := SCIMError{
		Schemas:  []string{SCIMSchemaError},
		Status:   strconv.Itoa(status),
		ScimType: scimType,
		Detail:   detail,
	}
	s.writeJSON(w, status, err)
}

// ServiceProviderConfig returns SCIM service provider configuration.
func (s *SCIMServer) ServiceProviderConfig() map[string]interface{} {
	return map[string]interface{}{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"documentationUri": s.baseURL + "/scim/v2/docs",
		"patch": map[string]bool{"supported": true},
		"bulk": map[string]interface{}{
			"supported":      false,
			"maxOperations":  0,
			"maxPayloadSize": 0,
		},
		"filter": map[string]interface{}{
			"supported":  true,
			"maxResults": 200,
		},
		"changePassword": map[string]bool{"supported": false},
		"sort":           map[string]bool{"supported": false},
		"etag":           map[string]bool{"supported": false},
		"authenticationSchemes": []map[string]interface{}{
			{
				"type":        "oauthbearertoken",
				"name":        "OAuth Bearer Token",
				"description": "Authentication scheme using the OAuth Bearer Token Standard",
				"specUri":     "http://www.rfc-editor.org/info/rfc6750",
				"primary":     true,
			},
		},
	}
}
