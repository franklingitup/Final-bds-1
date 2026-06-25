package inventory

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCollector_Collect(t *testing.T) {
	// Create a fake clientset with test data.
	clientset := fake.NewSimpleClientset(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "kube-system",
				UID:  types.UID("cluster-uid-123"),
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
				Labels: map[string]string{
					"topology.kubernetes.io/region":   "us-west-2",
					"eks.amazonaws.com/nodegroup":     "default",
					"node.kubernetes.io/instance-type": "m5.large",
				},
			},
			Spec: corev1.NodeSpec{
				ProviderID: "aws:///us-west-2a/i-1234567890abcdef0",
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-2",
				Labels: map[string]string{
					"topology.kubernetes.io/region": "us-west-2",
				},
			},
			Spec: corev1.NodeSpec{
				ProviderID: "aws:///us-west-2b/i-0987654321fedcba0",
			},
		},
	)

	// Set up fake discovery to return version.
	fakeDiscovery, ok := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	if !ok {
		t.Fatal("failed to get fake discovery client")
	}
	fakeDiscovery.FakedServerVersion = &version.Info{
		GitVersion: "v1.28.5",
	}

	collector := NewCollectorWithClient(clientset)
	info, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Verify collected info.
	if info.KubernetesVersion != "v1.28.5" {
		t.Errorf("KubernetesVersion = %q, want %q", info.KubernetesVersion, "v1.28.5")
	}
	if info.NodeCount != 2 {
		t.Errorf("NodeCount = %d, want %d", info.NodeCount, 2)
	}
	if info.ClusterUID != "cluster-uid-123" {
		t.Errorf("ClusterUID = %q, want %q", info.ClusterUID, "cluster-uid-123")
	}
	if info.CloudProvider != "aws" {
		t.Errorf("CloudProvider = %q, want %q", info.CloudProvider, "aws")
	}
	if info.Region != "us-west-2" {
		t.Errorf("Region = %q, want %q", info.Region, "us-west-2")
	}
	if !info.APIServerHealthy {
		t.Error("APIServerHealthy = false, want true")
	}
}

func TestDetectCloudProvider(t *testing.T) {
	tests := []struct {
		name       string
		labels     map[string]string
		providerID string
		want       string
	}{
		{
			name:       "AWS from provider ID",
			labels:     map[string]string{},
			providerID: "aws:///us-west-2a/i-1234567890abcdef0",
			want:       "aws",
		},
		{
			name:       "GCP from provider ID",
			labels:     map[string]string{},
			providerID: "gce:///projects/my-project/zones/us-central1-a/instances/instance-1",
			want:       "gcp",
		},
		{
			name:       "Azure from provider ID",
			labels:     map[string]string{},
			providerID: "azure:///subscriptions/xxx/resourceGroups/xxx/providers/Microsoft.Compute/virtualMachines/xxx",
			want:       "azure",
		},
		{
			name: "AWS from EKS label",
			labels: map[string]string{
				"eks.amazonaws.com/nodegroup": "default",
			},
			providerID: "",
			want:       "aws",
		},
		{
			name: "GCP from GKE label",
			labels: map[string]string{
				"cloud.google.com/gke-nodepool": "default-pool",
			},
			providerID: "",
			want:       "gcp",
		},
		{
			name: "Azure from AKS label",
			labels: map[string]string{
				"kubernetes.azure.com/cluster": "my-cluster",
			},
			providerID: "",
			want:       "azure",
		},
		{
			name:       "on-prem when nothing detected",
			labels:     map[string]string{},
			providerID: "",
			want:       "on-prem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectCloudProvider(tt.labels, tt.providerID)
			if got != tt.want {
				t.Errorf("detectCloudProvider() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectRegion(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name: "standard topology label",
			labels: map[string]string{
				"topology.kubernetes.io/region": "us-west-2",
			},
			want: "us-west-2",
		},
		{
			name: "legacy label",
			labels: map[string]string{
				"failure-domain.beta.kubernetes.io/region": "eu-west-1",
			},
			want: "eu-west-1",
		},
		{
			name:   "no region labels",
			labels: map[string]string{},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectRegion(tt.labels)
			if got != tt.want {
				t.Errorf("detectRegion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCollector_CheckHealth(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	fakeDiscovery, _ := clientset.Discovery().(*fakediscovery.FakeDiscovery)
	fakeDiscovery.FakedServerVersion = &version.Info{
		GitVersion: "v1.28.5",
	}

	collector := NewCollectorWithClient(clientset)
	if !collector.CheckHealth(context.Background()) {
		t.Error("CheckHealth() = false, want true")
	}
}
