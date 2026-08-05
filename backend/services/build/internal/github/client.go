package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClientConfig holds GitHub API client configuration.
type ClientConfig struct {
	BaseURL      string // GitHub API URL (default: https://api.github.com)
	ClientID     string // OAuth client ID
	ClientSecret string // OAuth client secret
	WebhookURL   string // Base URL for webhooks (e.g., https://api.example.com)
	HTTPTimeout  time.Duration
}

// DefaultClientConfig returns the default client configuration.
func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		BaseURL:     "https://api.github.com",
		HTTPTimeout: 30 * time.Second,
	}
}

// Client is a GitHub API client.
type Client struct {
	cfg        ClientConfig
	httpClient *http.Client
	token      string
}

// NewClient creates a new GitHub API client.
func NewClient(cfg ClientConfig, token string) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.github.com"
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 30 * time.Second
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: cfg.HTTPTimeout},
		token:      token,
	}
}

// WithToken returns a new client with the given token.
func (c *Client) WithToken(token string) *Client {
	return &Client{
		cfg:        c.cfg,
		httpClient: c.httpClient,
		token:      token,
	}
}

// ----------------------------------------------------------------------------
// GitHub API Types
// ----------------------------------------------------------------------------

// User represents a GitHub user.
type User struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	HTMLURL   string `json:"html_url"`
}

// Repository represents a GitHub repository.
type Repository struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	FullName      string   `json:"full_name"`
	Owner         User     `json:"owner"`
	Description   string   `json:"description"`
	HTMLURL       string   `json:"html_url"`
	CloneURL      string   `json:"clone_url"`
	SSHURL        string   `json:"ssh_url"`
	DefaultBranch string   `json:"default_branch"`
	Private       bool     `json:"private"`
	Fork          bool     `json:"fork"`
	Archived      bool     `json:"archived"`
	Disabled      bool     `json:"disabled"`
	StargazersCount int    `json:"stargazers_count"`
	ForksCount    int      `json:"forks_count"`
	WatchersCount int      `json:"watchers_count"`
	OpenIssuesCount int    `json:"open_issues_count"`
	Topics        []string `json:"topics"`
	Language      string   `json:"language"`
	Permissions   struct {
		Admin    bool `json:"admin"`
		Maintain bool `json:"maintain"`
		Push     bool `json:"push"`
		Triage   bool `json:"triage"`
		Pull     bool `json:"pull"`
	} `json:"permissions"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	PushedAt  time.Time `json:"pushed_at"`
}

// Branch represents a GitHub branch.
type Branch struct {
	Name      string `json:"name"`
	Commit    Commit `json:"commit"`
	Protected bool   `json:"protected"`
}

// Commit represents a GitHub commit.
type Commit struct {
	SHA    string `json:"sha"`
	URL    string `json:"url"`
	Commit struct {
		Message   string `json:"message"`
		Author    Author `json:"author"`
		Committer Author `json:"committer"`
	} `json:"commit"`
	HTMLURL string `json:"html_url"`
	Author  User   `json:"author"`
}

// Author represents a commit author.
type Author struct {
	Name  string    `json:"name"`
	Email string    `json:"email"`
	Date  time.Time `json:"date"`
}

// Hook represents a GitHub webhook.
type Hook struct {
	ID        int64    `json:"id"`
	URL       string   `json:"url"`
	PingURL   string   `json:"ping_url"`
	Name      string   `json:"name"`
	Events    []string `json:"events"`
	Active    bool     `json:"active"`
	Config    struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		Secret      string `json:"secret,omitempty"`
		InsecureSSL string `json:"insecure_ssl"`
	} `json:"config"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// OAuthToken represents an OAuth access token response.
type OAuthToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
}

// APIError represents a GitHub API error.
type APIError struct {
	Message          string `json:"message"`
	DocumentationURL string `json:"documentation_url"`
	StatusCode       int    `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github api error: %s (status %d)", e.Message, e.StatusCode)
}

// ----------------------------------------------------------------------------
// HTTP Methods
// ----------------------------------------------------------------------------

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, result any) error {
	u := c.cfg.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var apiErr APIError
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
			return &APIError{
				Message:    fmt.Sprintf("request failed with status %d", resp.StatusCode),
				StatusCode: resp.StatusCode,
			}
		}
		apiErr.StatusCode = resp.StatusCode
		return &apiErr
	}

	if result != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

func (c *Client) get(ctx context.Context, path string, result any) error {
	return c.do(ctx, http.MethodGet, path, nil, result)
}

func (c *Client) post(ctx context.Context, path string, body, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = strings.NewReader(string(data))
	}
	return c.do(ctx, http.MethodPost, path, bodyReader, result)
}

func (c *Client) patch(ctx context.Context, path string, body, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = strings.NewReader(string(data))
	}
	return c.do(ctx, http.MethodPatch, path, bodyReader, result)
}

func (c *Client) delete(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// ----------------------------------------------------------------------------
// User API
// ----------------------------------------------------------------------------

// GetAuthenticatedUser returns the authenticated user.
func (c *Client) GetAuthenticatedUser(ctx context.Context) (*User, error) {
	var user User
	if err := c.get(ctx, "/user", &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// ValidateToken validates the current token and returns the user.
func (c *Client) ValidateToken(ctx context.Context) (*User, []string, error) {
	user, err := c.GetAuthenticatedUser(ctx)
	if err != nil {
		return nil, nil, err
	}

	// The scopes are returned in the X-OAuth-Scopes header, but since we
	// don't have direct access to headers here, we'll return empty scopes.
	// In a production implementation, you'd use a custom HTTP client that
	// captures the response headers.
	return user, nil, nil
}

// ----------------------------------------------------------------------------
// Repository API
// ----------------------------------------------------------------------------

// GetRepository returns a repository by owner and name.
func (c *Client) GetRepository(ctx context.Context, owner, repo string) (*Repository, error) {
	var r Repository
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/%s", owner, repo), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListUserRepositories lists repositories for the authenticated user.
func (c *Client) ListUserRepositories(ctx context.Context, page, perPage int) ([]Repository, error) {
	path := fmt.Sprintf("/user/repos?type=all&sort=updated&page=%d&per_page=%d", page, perPage)
	var repos []Repository
	if err := c.get(ctx, path, &repos); err != nil {
		return nil, err
	}
	return repos, nil
}

// ListOrgRepositories lists repositories for an organization.
func (c *Client) ListOrgRepositories(ctx context.Context, org string, page, perPage int) ([]Repository, error) {
	path := fmt.Sprintf("/orgs/%s/repos?sort=updated&page=%d&per_page=%d", org, page, perPage)
	var repos []Repository
	if err := c.get(ctx, path, &repos); err != nil {
		return nil, err
	}
	return repos, nil
}

// GetRepositoryLanguages returns the languages used in a repository.
func (c *Client) GetRepositoryLanguages(ctx context.Context, owner, repo string) (map[string]int, error) {
	var languages map[string]int
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/languages", owner, repo), &languages); err != nil {
		return nil, err
	}
	return languages, nil
}

// ----------------------------------------------------------------------------
// Branch API
// ----------------------------------------------------------------------------

// ListBranches lists branches for a repository.
func (c *Client) ListBranches(ctx context.Context, owner, repo string, page, perPage int) ([]Branch, error) {
	path := fmt.Sprintf("/repos/%s/%s/branches?page=%d&per_page=%d", owner, repo, page, perPage)
	var branches []Branch
	if err := c.get(ctx, path, &branches); err != nil {
		return nil, err
	}
	return branches, nil
}

// GetBranch returns a specific branch.
func (c *Client) GetBranch(ctx context.Context, owner, repo, branch string) (*Branch, error) {
	var b Branch
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/branches/%s", owner, repo, branch), &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// GetDefaultBranch returns the default branch for a repository.
func (c *Client) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	r, err := c.GetRepository(ctx, owner, repo)
	if err != nil {
		return "", err
	}
	return r.DefaultBranch, nil
}

// ----------------------------------------------------------------------------
// Commit API
// ----------------------------------------------------------------------------

// ListCommits lists commits for a repository.
func (c *Client) ListCommits(ctx context.Context, owner, repo, sha string, page, perPage int) ([]Commit, error) {
	path := fmt.Sprintf("/repos/%s/%s/commits?page=%d&per_page=%d", owner, repo, page, perPage)
	if sha != "" {
		path += "&sha=" + url.QueryEscape(sha)
	}
	var commits []Commit
	if err := c.get(ctx, path, &commits); err != nil {
		return nil, err
	}
	return commits, nil
}

// GetCommit returns a specific commit.
func (c *Client) GetCommit(ctx context.Context, owner, repo, sha string) (*Commit, error) {
	var commit Commit
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/commits/%s", owner, repo, sha), &commit); err != nil {
		return nil, err
	}
	return &commit, nil
}

// ----------------------------------------------------------------------------
// Webhook API
// ----------------------------------------------------------------------------

// CreateWebhook creates a webhook on a repository.
func (c *Client) CreateWebhook(ctx context.Context, owner, repo, webhookURL, secret string, events []string) (*Hook, error) {
	body := map[string]any{
		"name":   "web",
		"active": true,
		"events": events,
		"config": map[string]any{
			"url":          webhookURL,
			"content_type": "json",
			"secret":       secret,
			"insecure_ssl": "0",
		},
	}

	var hook Hook
	if err := c.post(ctx, fmt.Sprintf("/repos/%s/%s/hooks", owner, repo), body, &hook); err != nil {
		return nil, err
	}
	return &hook, nil
}

// UpdateWebhook updates a webhook.
func (c *Client) UpdateWebhook(ctx context.Context, owner, repo string, hookID int64, webhookURL, secret string, events []string, active bool) (*Hook, error) {
	body := map[string]any{
		"active": active,
		"events": events,
		"config": map[string]any{
			"url":          webhookURL,
			"content_type": "json",
			"secret":       secret,
			"insecure_ssl": "0",
		},
	}

	var hook Hook
	if err := c.patch(ctx, fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, hookID), body, &hook); err != nil {
		return nil, err
	}
	return &hook, nil
}

// DeleteWebhook deletes a webhook.
func (c *Client) DeleteWebhook(ctx context.Context, owner, repo string, hookID int64) error {
	return c.delete(ctx, fmt.Sprintf("/repos/%s/%s/hooks/%d", owner, repo, hookID))
}

// ListWebhooks lists webhooks for a repository.
func (c *Client) ListWebhooks(ctx context.Context, owner, repo string) ([]Hook, error) {
	var hooks []Hook
	if err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/hooks", owner, repo), &hooks); err != nil {
		return nil, err
	}
	return hooks, nil
}

// PingWebhook pings a webhook.
func (c *Client) PingWebhook(ctx context.Context, owner, repo string, hookID int64) error {
	return c.post(ctx, fmt.Sprintf("/repos/%s/%s/hooks/%d/pings", owner, repo, hookID), nil, nil)
}

// ----------------------------------------------------------------------------
// OAuth API
// ----------------------------------------------------------------------------

// GetOAuthURL returns the URL to redirect users to for OAuth authorization.
func (c *Client) GetOAuthURL(state, redirectURI string, scopes []string) string {
	params := url.Values{
		"client_id":    {c.cfg.ClientID},
		"state":        {state},
		"scope":        {strings.Join(scopes, " ")},
	}
	if redirectURI != "" {
		params.Set("redirect_uri", redirectURI)
	}
	return "https://github.com/login/oauth/authorize?" + params.Encode()
}

// ExchangeCode exchanges an OAuth code for an access token.
func (c *Client) ExchangeCode(ctx context.Context, code string) (*OAuthToken, error) {
	data := url.Values{
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"code":          {code},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://github.com/login/oauth/access_token",
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("oauth token exchange failed: %s", string(body))
	}

	var token OAuthToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &token, nil
}

// RefreshToken refreshes an OAuth access token.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (*OAuthToken, error) {
	data := url.Values{
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://github.com/login/oauth/access_token",
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("oauth token refresh failed: %s", string(body))
	}

	var token OAuthToken
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &token, nil
}
