// Package inventory collects information about the Kubernetes cluster.
package inventory

import (
	"context"
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Info contains cluster inventory information.
type Info struct {
	KubernetesVersion string
	NodeCount         int
	ClusterUID        string
	CloudProvider     string
	Region            string
	APIServerHealthy  bool
}

// Collector gathers inventory information from the cluster.
type Collector struct {
	clientset kubernetes.Interface
}

// NewCollector creates a new inventory collector.
// It uses in-cluster configuration when running inside a pod.
func NewCollector() (*Collector, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	return &Collector{clientset: clientset}, nil
}

// NewCollectorWithClient creates a collector with an existing client (for testing).
func NewCollectorWithClient(clientset kubernetes.Interface) *Collector {
	return &Collector{clientset: clientset}
}

// Collect gathers current cluster inventory.
func (c *Collector) Collect(ctx context.Context) (*Info, error) {
	info := &Info{}

	// Get Kubernetes version from server.
	version, err := c.clientset.Discovery().ServerVersion()
	if err != nil {
		info.APIServerHealthy = false
		return info, fmt.Errorf("get server version: %w", err)
	}
	info.KubernetesVersion = version.GitVersion
	info.APIServerHealthy = true

	// Count nodes.
	nodes, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return info, fmt.Errorf("list nodes: %w", err)
	}
	info.NodeCount = len(nodes.Items)

	// Get cluster UID from kube-system namespace.
	ns, err := c.clientset.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		return info, fmt.Errorf("get kube-system namespace: %w", err)
	}
	info.ClusterUID = string(ns.UID)

	// Detect cloud provider and region from node labels.
	if len(nodes.Items) > 0 {
		node := nodes.Items[0]
		info.CloudProvider = detectCloudProvider(node.Labels, node.Spec.ProviderID)
		info.Region = detectRegion(node.Labels)
	}

	return info, nil
}

// CheckHealth performs a quick health check of the API server.
func (c *Collector) CheckHealth(ctx context.Context) bool {
	_, err := c.clientset.Discovery().ServerVersion()
	return err == nil
}

// detectCloudProvider determines the cloud provider from node metadata.
func detectCloudProvider(labels map[string]string, providerID string) string {
	// Check provider ID prefix.
	if strings.HasPrefix(providerID, "aws://") {
		return "aws"
	}
	if strings.HasPrefix(providerID, "gce://") || strings.HasPrefix(providerID, "gcp://") {
		return "gcp"
	}
	if strings.HasPrefix(providerID, "azure://") {
		return "azure"
	}

	// Check node labels.
	if _, ok := labels["eks.amazonaws.com/nodegroup"]; ok {
		return "aws"
	}
	if _, ok := labels["cloud.google.com/gke-nodepool"]; ok {
		return "gcp"
	}
	if _, ok := labels["kubernetes.azure.com/cluster"]; ok {
		return "azure"
	}

	// Check common provider labels.
	if provider, ok := labels["node.kubernetes.io/instance-type"]; ok {
		if strings.Contains(provider, "aws") {
			return "aws"
		}
	}

	// Check environment variable override.
	if provider := os.Getenv("CLOUD_PROVIDER"); provider != "" {
		return provider
	}

	return "on-prem"
}

// detectRegion determines the region from node labels.
func detectRegion(labels map[string]string) string {
	// Standard Kubernetes topology label.
	if region, ok := labels["topology.kubernetes.io/region"]; ok {
		return region
	}

	// Legacy label.
	if region, ok := labels["failure-domain.beta.kubernetes.io/region"]; ok {
		return region
	}

	// AWS specific.
	if region, ok := labels["topology.ebs.csi.aws.com/zone"]; ok {
		// Zone is like "us-west-2a", extract region "us-west-2".
		if len(region) > 1 {
			return region[:len(region)-1]
		}
	}

	// Check environment variable override.
	if region := os.Getenv("CLOUD_REGION"); region != "" {
		return region
	}

	return ""
}
