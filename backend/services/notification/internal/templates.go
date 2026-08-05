package notification

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// TemplateEngine renders notification templates.
type TemplateEngine struct {
	funcMap template.FuncMap
}

// NewTemplateEngine creates a new template engine.
func NewTemplateEngine() *TemplateEngine {
	return &TemplateEngine{
		funcMap: template.FuncMap{
			"upper":    strings.ToUpper,
			"lower":    strings.ToLower,
			"title":    strings.Title,
			"trim":     strings.TrimSpace,
			"default":  defaultValue,
			"truncate": truncate,
			"emoji":    severityEmoji,
		},
	}
}

// RenderData contains data for template rendering.
type RenderData struct {
	// Common fields
	OrgName      string
	ProjectName  string
	ResourceType string
	ResourceName string
	ResourceID   string
	
	// Event-specific
	EventType    string
	Severity     string
	Title        string
	Body         string
	
	// Deployment-specific
	DeploymentName string
	Version        string
	Environment    string
	ClusterName    string
	Namespace      string
	
	// Build-specific
	BuildID     string
	Repository  string
	Branch      string
	CommitSHA   string
	
	// Alert-specific
	AlertName   string
	AlertRule   string
	MetricValue string
	Threshold   string
	
	// User-specific
	UserName    string
	UserEmail   string
	
	// URLs
	DashboardURL string
	LogsURL      string
	
	// Custom data
	Custom map[string]interface{}
}

// Render renders a template with the given data.
func (e *TemplateEngine) Render(tmplText string, data *RenderData) (string, error) {
	tmpl, err := template.New("notification").Funcs(e.funcMap).Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}

// RenderSubject renders a template subject line.
func (e *TemplateEngine) RenderSubject(tmplText string, data *RenderData) (string, error) {
	result, err := e.Render(tmplText, data)
	if err != nil {
		return "", err
	}
	// Clean up subject - single line, no extra whitespace
	return strings.TrimSpace(strings.ReplaceAll(result, "\n", " ")), nil
}

// DefaultTemplates returns the default notification templates.
var DefaultTemplates = map[string]struct {
	Subject  string
	BodyText string
	BodyHTML string
}{
	EventDeploymentCompleted: {
		Subject:  "✅ Deployment Completed: {{.DeploymentName}}",
		BodyText: `Deployment completed successfully.

Deployment: {{.DeploymentName}}
Environment: {{.Environment}}
Version: {{.Version}}
Cluster: {{.ClusterName}}
Namespace: {{.Namespace}}

View deployment: {{.DashboardURL}}`,
		BodyHTML: `<h2>✅ Deployment Completed</h2>
<p>Deployment completed successfully.</p>
<table>
<tr><td><strong>Deployment:</strong></td><td>{{.DeploymentName}}</td></tr>
<tr><td><strong>Environment:</strong></td><td>{{.Environment}}</td></tr>
<tr><td><strong>Version:</strong></td><td>{{.Version}}</td></tr>
<tr><td><strong>Cluster:</strong></td><td>{{.ClusterName}}</td></tr>
<tr><td><strong>Namespace:</strong></td><td>{{.Namespace}}</td></tr>
</table>
<p><a href="{{.DashboardURL}}">View Deployment</a></p>`,
	},
	EventDeploymentFailed: {
		Subject:  "❌ Deployment Failed: {{.DeploymentName}}",
		BodyText: `Deployment failed.

Deployment: {{.DeploymentName}}
Environment: {{.Environment}}
Version: {{.Version}}
Cluster: {{.ClusterName}}
Namespace: {{.Namespace}}

Error: {{.Body}}

View logs: {{.LogsURL}}`,
		BodyHTML: `<h2>❌ Deployment Failed</h2>
<p>Deployment failed.</p>
<table>
<tr><td><strong>Deployment:</strong></td><td>{{.DeploymentName}}</td></tr>
<tr><td><strong>Environment:</strong></td><td>{{.Environment}}</td></tr>
<tr><td><strong>Version:</strong></td><td>{{.Version}}</td></tr>
<tr><td><strong>Cluster:</strong></td><td>{{.ClusterName}}</td></tr>
<tr><td><strong>Namespace:</strong></td><td>{{.Namespace}}</td></tr>
</table>
<p><strong>Error:</strong> {{.Body}}</p>
<p><a href="{{.LogsURL}}">View Logs</a></p>`,
	},
	EventDeploymentStarted: {
		Subject:  "🚀 Deployment Started: {{.DeploymentName}}",
		BodyText: `Deployment started.

Deployment: {{.DeploymentName}}
Environment: {{.Environment}}
Version: {{.Version}}
Cluster: {{.ClusterName}}

View deployment: {{.DashboardURL}}`,
		BodyHTML: `<h2>🚀 Deployment Started</h2>
<p>A new deployment has started.</p>
<table>
<tr><td><strong>Deployment:</strong></td><td>{{.DeploymentName}}</td></tr>
<tr><td><strong>Environment:</strong></td><td>{{.Environment}}</td></tr>
<tr><td><strong>Version:</strong></td><td>{{.Version}}</td></tr>
<tr><td><strong>Cluster:</strong></td><td>{{.ClusterName}}</td></tr>
</table>
<p><a href="{{.DashboardURL}}">View Deployment</a></p>`,
	},
	EventBuildCompleted: {
		Subject:  "✅ Build Completed: {{.Repository}}",
		BodyText: `Build completed successfully.

Repository: {{.Repository}}
Branch: {{.Branch}}
Commit: {{.CommitSHA | truncate 8}}
Build ID: {{.BuildID}}

View build: {{.DashboardURL}}`,
		BodyHTML: `<h2>✅ Build Completed</h2>
<p>Build completed successfully.</p>
<table>
<tr><td><strong>Repository:</strong></td><td>{{.Repository}}</td></tr>
<tr><td><strong>Branch:</strong></td><td>{{.Branch}}</td></tr>
<tr><td><strong>Commit:</strong></td><td>{{.CommitSHA | truncate 8}}</td></tr>
<tr><td><strong>Build ID:</strong></td><td>{{.BuildID}}</td></tr>
</table>
<p><a href="{{.DashboardURL}}">View Build</a></p>`,
	},
	EventBuildFailed: {
		Subject:  "❌ Build Failed: {{.Repository}}",
		BodyText: `Build failed.

Repository: {{.Repository}}
Branch: {{.Branch}}
Commit: {{.CommitSHA | truncate 8}}
Build ID: {{.BuildID}}

Error: {{.Body}}

View logs: {{.LogsURL}}`,
		BodyHTML: `<h2>❌ Build Failed</h2>
<p>Build failed.</p>
<table>
<tr><td><strong>Repository:</strong></td><td>{{.Repository}}</td></tr>
<tr><td><strong>Branch:</strong></td><td>{{.Branch}}</td></tr>
<tr><td><strong>Commit:</strong></td><td>{{.CommitSHA | truncate 8}}</td></tr>
<tr><td><strong>Build ID:</strong></td><td>{{.BuildID}}</td></tr>
</table>
<p><strong>Error:</strong> {{.Body}}</p>
<p><a href="{{.LogsURL}}">View Logs</a></p>`,
	},
	EventAlertFiring: {
		Subject:  "🔴 Alert Firing: {{.AlertName}}",
		BodyText: `Alert is firing.

Alert: {{.AlertName}}
Rule: {{.AlertRule}}
Value: {{.MetricValue}}
Threshold: {{.Threshold}}
Severity: {{.Severity | upper}}

{{.Body}}

View alert: {{.DashboardURL}}`,
		BodyHTML: `<h2>🔴 Alert Firing</h2>
<p>An alert is firing.</p>
<table>
<tr><td><strong>Alert:</strong></td><td>{{.AlertName}}</td></tr>
<tr><td><strong>Rule:</strong></td><td>{{.AlertRule}}</td></tr>
<tr><td><strong>Value:</strong></td><td>{{.MetricValue}}</td></tr>
<tr><td><strong>Threshold:</strong></td><td>{{.Threshold}}</td></tr>
<tr><td><strong>Severity:</strong></td><td>{{.Severity | upper}}</td></tr>
</table>
<p>{{.Body}}</p>
<p><a href="{{.DashboardURL}}">View Alert</a></p>`,
	},
	EventAlertResolved: {
		Subject:  "✅ Alert Resolved: {{.AlertName}}",
		BodyText: `Alert has been resolved.

Alert: {{.AlertName}}
Rule: {{.AlertRule}}

View alert: {{.DashboardURL}}`,
		BodyHTML: `<h2>✅ Alert Resolved</h2>
<p>The alert has been resolved.</p>
<table>
<tr><td><strong>Alert:</strong></td><td>{{.AlertName}}</td></tr>
<tr><td><strong>Rule:</strong></td><td>{{.AlertRule}}</td></tr>
</table>
<p><a href="{{.DashboardURL}}">View Alert</a></p>`,
	},
	EventInvitationSent: {
		Subject:  "You've been invited to {{.OrgName}}",
		BodyText: `Hello,

You've been invited to join {{.OrgName}} on BDS Platform.

Click the link below to accept the invitation:
{{.DashboardURL}}

Best regards,
BDS Platform Team`,
		BodyHTML: `<h2>You've been invited!</h2>
<p>Hello,</p>
<p>You've been invited to join <strong>{{.OrgName}}</strong> on BDS Platform.</p>
<p><a href="{{.DashboardURL}}" style="display:inline-block;padding:10px 20px;background:#4F46E5;color:#fff;text-decoration:none;border-radius:5px;">Accept Invitation</a></p>
<p>Best regards,<br>BDS Platform Team</p>`,
	},
}

// DefaultSlackBlocks returns Slack Block Kit templates.
var DefaultSlackBlocks = map[string][]interface{}{
	EventDeploymentCompleted: {
		map[string]interface{}{
			"type": "header",
			"text": map[string]interface{}{
				"type": "plain_text",
				"text": "✅ Deployment Completed",
			},
		},
		map[string]interface{}{
			"type": "section",
			"fields": []interface{}{
				map[string]interface{}{"type": "mrkdwn", "text": "*Deployment:*\n{{.DeploymentName}}"},
				map[string]interface{}{"type": "mrkdwn", "text": "*Environment:*\n{{.Environment}}"},
				map[string]interface{}{"type": "mrkdwn", "text": "*Version:*\n{{.Version}}"},
				map[string]interface{}{"type": "mrkdwn", "text": "*Cluster:*\n{{.ClusterName}}"},
			},
		},
		map[string]interface{}{
			"type": "actions",
			"elements": []interface{}{
				map[string]interface{}{
					"type": "button",
					"text": map[string]interface{}{"type": "plain_text", "text": "View Deployment"},
					"url":  "{{.DashboardURL}}",
				},
			},
		},
	},
	EventDeploymentFailed: {
		map[string]interface{}{
			"type": "header",
			"text": map[string]interface{}{
				"type": "plain_text",
				"text": "❌ Deployment Failed",
			},
		},
		map[string]interface{}{
			"type": "section",
			"fields": []interface{}{
				map[string]interface{}{"type": "mrkdwn", "text": "*Deployment:*\n{{.DeploymentName}}"},
				map[string]interface{}{"type": "mrkdwn", "text": "*Environment:*\n{{.Environment}}"},
			},
		},
		map[string]interface{}{
			"type": "section",
			"text": map[string]interface{}{
				"type": "mrkdwn",
				"text": "*Error:* {{.Body}}",
			},
		},
		map[string]interface{}{
			"type": "actions",
			"elements": []interface{}{
				map[string]interface{}{
					"type": "button",
					"text": map[string]interface{}{"type": "plain_text", "text": "View Logs"},
					"url":  "{{.LogsURL}}",
				},
			},
		},
	},
}

// Helper functions for templates
func defaultValue(def interface{}, val interface{}) interface{} {
	if val == nil || val == "" {
		return def
	}
	return val
}

func truncate(length int, s string) string {
	if len(s) <= length {
		return s
	}
	return s[:length]
}

func severityEmoji(severity string) string {
	switch strings.ToLower(severity) {
	case SeverityInfo:
		return "ℹ️"
	case SeverityWarning:
		return "⚠️"
	case SeverityError:
		return "❌"
	case SeverityCritical:
		return "🔴"
	default:
		return "📣"
	}
}
