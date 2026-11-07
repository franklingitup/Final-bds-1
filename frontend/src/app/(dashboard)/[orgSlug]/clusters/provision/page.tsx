"use client";

import * as React from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";
import {
  ArrowLeft,
  ArrowRight,
  Check,
  ChevronRight,
  Cloud,
  Loader2,
  Plus,
  Server,
  Trash2,
  AlertTriangle,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { LoadingState } from "@/components/shared/loading-state";
import { useOrganization } from "@/providers/organization-provider";
import { provisioningApi } from "@/lib/api";
import type {
  CredentialView,
  TemplateView,
  RegionInfo,
  MachineTypeInfo,
  K8sVersionInfo,
  NodePool,
  NetworkingConfig,
  ClusterAddon,
  ProvisioningRequestView,
} from "@/lib/api/provisioning";
import { Provider, RequestStatus } from "@/lib/api/provisioning";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "@/hooks/use-toast";
import { cn } from "@/lib/utils";

type WizardStep = "provider" | "config" | "review" | "confirm";

const STEPS: { id: WizardStep; title: string; description: string }[] = [
  { id: "provider", title: "Provider & Region", description: "Select cloud provider and region" },
  { id: "config", title: "Cluster Configuration", description: "Configure node pools and networking" },
  { id: "review", title: "Review", description: "Review Terraform configuration" },
  { id: "confirm", title: "Confirm & Start", description: "Start cluster provisioning" },
];

const DEFAULT_NODE_POOL: Omit<NodePool, "name"> = {
  machineType: "",
  minNodes: 1,
  maxNodes: 3,
  desiredNodes: 2,
  diskSizeGb: 100,
  diskType: "standard",
  preemptible: false,
  labels: {},
  taints: [],
};

const DEFAULT_NETWORKING: NetworkingConfig = {
  vpcCidr: "10.0.0.0/16",
  podCidr: "10.244.0.0/16",
  serviceCidr: "10.96.0.0/12",
  privateCluster: false,
  natGateway: true,
};

export default function ProvisionClusterPage() {
  const params = useParams();
  const router = useRouter();
  const orgSlug = params.orgSlug as string;
  const { organization } = useOrganization();
  const orgId = organization?.id || "";
  const queryClient = useQueryClient();

  // Wizard state
  const [currentStep, setCurrentStep] = React.useState<WizardStep>("provider");
  const [clusterName, setClusterName] = React.useState("");
  const [selectedProvider, setSelectedProvider] = React.useState<string>("");
  const [selectedRegion, setSelectedRegion] = React.useState<string>("");
  const [selectedCredential, setSelectedCredential] = React.useState<string>("");
  const [selectedTemplate, setSelectedTemplate] = React.useState<string>("");
  const [selectedK8sVersion, setSelectedK8sVersion] = React.useState<string>("");
  const [nodePools, setNodePools] = React.useState<NodePool[]>([
    { ...DEFAULT_NODE_POOL, name: "default" },
  ]);
  const [networking, setNetworking] = React.useState<NetworkingConfig>(DEFAULT_NETWORKING);
  const [addons, setAddons] = React.useState<ClusterAddon[]>([]);
  const [provisioningRequest, setProvisioningRequest] = React.useState<ProvisioningRequestView | null>(null);

  // Fetch credentials
  const { data: credentialsData, isLoading: credentialsLoading } = useQuery({
    queryKey: ["credentials", orgId],
    queryFn: () => provisioningApi.listCredentials(orgId),
    enabled: !!orgId,
  });

  // Fetch templates
  const { data: templatesData, isLoading: templatesLoading } = useQuery({
    queryKey: ["templates", orgId, selectedProvider],
    queryFn: () => provisioningApi.listTemplates(orgId, selectedProvider || undefined),
    enabled: !!orgId && !!selectedProvider,
  });

  // Fetch regions
  const { data: regionsData, isLoading: regionsLoading } = useQuery({
    queryKey: ["regions", orgId, selectedProvider, selectedCredential],
    queryFn: () =>
      provisioningApi.listRegions(orgId, selectedProvider, selectedCredential || undefined),
    enabled: !!orgId && !!selectedProvider,
  });

  // Fetch machine types
  const { data: machineTypesData, isLoading: machineTypesLoading } = useQuery({
    queryKey: ["machineTypes", orgId, selectedProvider, selectedRegion],
    queryFn: () => provisioningApi.listMachineTypes(orgId, selectedProvider, selectedRegion),
    enabled: !!orgId && !!selectedProvider && !!selectedRegion,
  });

  // Fetch K8s versions
  const { data: k8sVersionsData, isLoading: k8sVersionsLoading } = useQuery({
    queryKey: ["k8sVersions", orgId, selectedProvider],
    queryFn: () => provisioningApi.listKubernetesVersions(orgId, selectedProvider),
    enabled: !!orgId && !!selectedProvider,
  });

  // Create provisioning request mutation
  const createProvisioningRequest = useMutation({
    mutationFn: (data: Parameters<typeof provisioningApi.createProvisioningRequest>[1]) =>
      provisioningApi.createProvisioningRequest(orgId, data),
    onSuccess: async (data) => {
      setProvisioningRequest(data);
      // Immediately generate terraform
      await generateTerraform.mutateAsync(data.id);
    },
    onError: (err: Error) => {
      toast({
        title: "Failed to create provisioning request",
        description: err.message,
        variant: "destructive",
      });
    },
  });

  // Generate terraform mutation
  const generateTerraform = useMutation({
    mutationFn: (requestId: string) => provisioningApi.generateTerraform(orgId, requestId),
    onSuccess: (data) => {
      setProvisioningRequest(data);
      setCurrentStep("review");
    },
    onError: (err: Error) => {
      toast({
        title: "Failed to generate Terraform",
        description: err.message,
        variant: "destructive",
      });
    },
  });

  // Start provisioning mutation
  const startProvisioning = useMutation({
    mutationFn: (requestId: string) => provisioningApi.startProvisioning(orgId, requestId),
    onSuccess: (data) => {
      toast({ title: "Cluster provisioning started" });
      // Navigate to the progress page
      router.push(`/${orgSlug}/clusters/provision/${data.id}/progress`);
    },
    onError: (err: Error) => {
      toast({
        title: "Failed to start provisioning",
        description: err.message,
        variant: "destructive",
      });
    },
  });

  // Filter credentials by provider
  const filteredCredentials = React.useMemo(() => {
    if (!credentialsData?.credentials || !selectedProvider) return [];
    return credentialsData.credentials.filter((c) => c.provider === selectedProvider);
  }, [credentialsData, selectedProvider]);

  // When provider changes, reset dependent fields
  React.useEffect(() => {
    setSelectedRegion("");
    setSelectedCredential("");
    setSelectedTemplate("");
    setSelectedK8sVersion("");
    // Reset node pool machine types
    setNodePools((pools) =>
      pools.map((p) => ({ ...p, machineType: "" }))
    );
  }, [selectedProvider]);

  // When template is selected, apply its configuration
  React.useEffect(() => {
    if (!selectedTemplate || !templatesData?.templates) return;
    const template = templatesData.templates.find((t) => t.id === selectedTemplate);
    if (template) {
      if (template.k8sVersion) setSelectedK8sVersion(template.k8sVersion);
      if (template.nodePools && template.nodePools.length > 0) setNodePools(template.nodePools);
      if (template.networking) setNetworking(template.networking);
      if (template.addons) setAddons(template.addons);
    }
  }, [selectedTemplate, templatesData]);

  // Auto-select default K8s version
  React.useEffect(() => {
    if (k8sVersionsData?.versions && !selectedK8sVersion) {
      const defaultVersion = k8sVersionsData.versions.find((v) => v.isDefault);
      if (defaultVersion) setSelectedK8sVersion(defaultVersion.version);
    }
  }, [k8sVersionsData, selectedK8sVersion]);

  // Node pool management
  const addNodePool = () => {
    setNodePools((pools) => [
      ...pools,
      { ...DEFAULT_NODE_POOL, name: `pool-${pools.length + 1}` },
    ]);
  };

  const removeNodePool = (index: number) => {
    if (nodePools.length === 1) {
      toast({ title: "At least one node pool is required", variant: "destructive" });
      return;
    }
    setNodePools((pools) => pools.filter((_, i) => i !== index));
  };

  const updateNodePool = (index: number, updates: Partial<NodePool>) => {
    setNodePools((pools) =>
      pools.map((p, i) => (i === index ? { ...p, ...updates } : p))
    );
  };

  // Step validation
  const canProceedFromProvider = () => {
    return (
      clusterName.trim().length >= 3 &&
      selectedProvider &&
      selectedRegion &&
      selectedCredential
    );
  };

  const canProceedFromConfig = () => {
    return (
      selectedK8sVersion &&
      nodePools.every((p) => p.name && p.machineType && p.minNodes >= 0 && p.maxNodes >= p.minNodes)
    );
  };

  // Step navigation
  const goToStep = (step: WizardStep) => {
    const currentIndex = STEPS.findIndex((s) => s.id === currentStep);
    const targetIndex = STEPS.findIndex((s) => s.id === step);

    // Can only go backwards freely, forwards requires validation
    if (targetIndex < currentIndex) {
      setCurrentStep(step);
    }
  };

  const handleNext = async () => {
    switch (currentStep) {
      case "provider":
        if (canProceedFromProvider()) {
          setCurrentStep("config");
        }
        break;
      case "config":
        if (canProceedFromConfig()) {
          // Create provisioning request and generate terraform
          createProvisioningRequest.mutate({
            name: clusterName,
            provider: selectedProvider,
            region: selectedRegion,
            credentialId: selectedCredential,
            templateId: selectedTemplate || undefined,
            k8sVersion: selectedK8sVersion,
            nodePools,
            networking,
            addons: addons.length > 0 ? addons : undefined,
          });
        }
        break;
      case "review":
        setCurrentStep("confirm");
        break;
      case "confirm":
        if (provisioningRequest) {
          startProvisioning.mutate(provisioningRequest.id);
        }
        break;
    }
  };

  const handleBack = () => {
    const currentIndex = STEPS.findIndex((s) => s.id === currentStep);
    if (currentIndex > 0) {
      setCurrentStep(STEPS[currentIndex - 1].id);
    }
  };

  const getProviderLabel = (provider: string) => {
    switch (provider) {
      case Provider.AWS:
        return "Amazon EKS";
      case Provider.GCP:
        return "Google GKE";
      case Provider.AZURE:
        return "Azure AKS";
      default:
        return provider;
    }
  };

  const isLoading =
    createProvisioningRequest.isPending ||
    generateTerraform.isPending ||
    startProvisioning.isPending;

  if (!organization) {
    return <LoadingState />;
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Provision New Cluster"
        description="Create a new Kubernetes cluster in your cloud provider"
        action={
          <Link href={`/${orgSlug}/clusters`}>
            <Button variant="outline">
              <ArrowLeft className="mr-2 h-4 w-4" />
              Back to Clusters
            </Button>
          </Link>
        }
      />

      {/* Progress Steps */}
      <div className="flex items-center justify-center mb-8">
        <nav aria-label="Progress" className="w-full max-w-3xl">
          <ol className="flex items-center">
            {STEPS.map((step, stepIdx) => {
              const isCurrent = step.id === currentStep;
              const isPast = STEPS.findIndex((s) => s.id === currentStep) > stepIdx;
              return (
                <li
                  key={step.id}
                  className={cn(
                    "relative flex-1",
                    stepIdx !== STEPS.length - 1 && "pr-8 sm:pr-20"
                  )}
                >
                  {stepIdx !== STEPS.length - 1 && (
                    <div
                      className={cn(
                        "absolute top-4 left-7 -right-8 sm:left-8 sm:-right-12 h-0.5",
                        isPast ? "bg-primary" : "bg-muted"
                      )}
                    />
                  )}
                  <button
                    type="button"
                    onClick={() => goToStep(step.id)}
                    className={cn(
                      "relative flex flex-col items-center group",
                      !isPast && !isCurrent && "cursor-not-allowed opacity-50"
                    )}
                    disabled={!isPast && !isCurrent}
                  >
                    <span
                      className={cn(
                        "h-8 w-8 rounded-full flex items-center justify-center text-sm font-medium",
                        isCurrent
                          ? "bg-primary text-primary-foreground"
                          : isPast
                          ? "bg-primary text-primary-foreground"
                          : "bg-muted text-muted-foreground"
                      )}
                    >
                      {isPast ? <Check className="h-4 w-4" /> : stepIdx + 1}
                    </span>
                    <span
                      className={cn(
                        "mt-2 text-xs font-medium hidden sm:block",
                        isCurrent ? "text-primary" : "text-muted-foreground"
                      )}
                    >
                      {step.title}
                    </span>
                  </button>
                </li>
              );
            })}
          </ol>
        </nav>
      </div>

      {/* Step Content */}
      <Card className="max-w-4xl mx-auto">
        {currentStep === "provider" && (
          <>
            <CardHeader>
              <CardTitle>Provider & Region</CardTitle>
              <CardDescription>
                Choose your cloud provider, region, and credentials
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="space-y-2">
                <Label htmlFor="cluster-name">Cluster Name</Label>
                <Input
                  id="cluster-name"
                  placeholder="my-production-cluster"
                  value={clusterName}
                  onChange={(e) => setClusterName(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  Must be at least 3 characters, lowercase letters, numbers, and hyphens only
                </p>
              </div>

              <div className="space-y-2">
                <Label>Cloud Provider</Label>
                <div className="grid grid-cols-3 gap-4">
                  {[Provider.AWS, Provider.GCP, Provider.AZURE].map((provider) => (
                    <button
                      key={provider}
                      type="button"
                      onClick={() => setSelectedProvider(provider)}
                      className={cn(
                        "p-4 border rounded-lg text-center transition-colors",
                        selectedProvider === provider
                          ? "border-primary bg-primary/5"
                          : "border-muted hover:border-primary/50"
                      )}
                    >
                      <Cloud className="h-8 w-8 mx-auto mb-2" />
                      <p className="font-medium">{getProviderLabel(provider)}</p>
                    </button>
                  ))}
                </div>
              </div>

              {selectedProvider && (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="credential">Cloud Credential</Label>
                    {credentialsLoading ? (
                      <LoadingState message="Loading credentials..." />
                    ) : filteredCredentials.length === 0 ? (
                      <div className="p-4 border rounded-lg bg-muted/50 text-center">
                        <p className="text-sm text-muted-foreground mb-2">
                          No credentials found for {getProviderLabel(selectedProvider)}
                        </p>
                        <Link href={`/${orgSlug}/settings`}>
                          <Button variant="outline" size="sm">
                            Add Credentials
                          </Button>
                        </Link>
                      </div>
                    ) : (
                      <Select
                        value={selectedCredential}
                        onValueChange={setSelectedCredential}
                      >
                        <SelectTrigger id="credential">
                          <SelectValue placeholder="Select credential" />
                        </SelectTrigger>
                        <SelectContent>
                          {filteredCredentials.map((cred) => (
                            <SelectItem key={cred.id} value={cred.id}>
                              <span className="flex items-center gap-2">
                                {cred.name}
                                {cred.validated && (
                                  <Badge variant="outline" className="text-xs">
                                    Validated
                                  </Badge>
                                )}
                              </span>
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    )}
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="region">Region</Label>
                    {regionsLoading ? (
                      <LoadingState message="Loading regions..." />
                    ) : (
                      <Select value={selectedRegion} onValueChange={setSelectedRegion}>
                        <SelectTrigger id="region">
                          <SelectValue placeholder="Select region" />
                        </SelectTrigger>
                        <SelectContent>
                          {regionsData?.regions?.map((region) => (
                            <SelectItem
                              key={region.code}
                              value={region.code}
                              disabled={!region.available}
                            >
                              {region.name} ({region.code})
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    )}
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="template">Cluster Template (Optional)</Label>
                    {templatesLoading ? (
                      <LoadingState message="Loading templates..." />
                    ) : (
                      <Select
                        value={selectedTemplate}
                        onValueChange={setSelectedTemplate}
                      >
                        <SelectTrigger id="template">
                          <SelectValue placeholder="Start from scratch or select a template" />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="">Start from scratch</SelectItem>
                          {templatesData?.templates?.map((template) => (
                            <SelectItem key={template.id} value={template.id}>
                              <span className="flex items-center gap-2">
                                {template.name}
                                {template.isDefault && (
                                  <Badge variant="secondary" className="text-xs">
                                    Default
                                  </Badge>
                                )}
                              </span>
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    )}
                    <p className="text-xs text-muted-foreground">
                      Templates provide pre-configured node pools, networking, and addons
                    </p>
                  </div>
                </>
              )}
            </CardContent>
          </>
        )}

        {currentStep === "config" && (
          <>
            <CardHeader>
              <CardTitle>Cluster Configuration</CardTitle>
              <CardDescription>
                Configure Kubernetes version, node pools, and networking
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="space-y-2">
                <Label htmlFor="k8s-version">Kubernetes Version</Label>
                {k8sVersionsLoading ? (
                  <LoadingState message="Loading versions..." />
                ) : (
                  <Select value={selectedK8sVersion} onValueChange={setSelectedK8sVersion}>
                    <SelectTrigger id="k8s-version">
                      <SelectValue placeholder="Select Kubernetes version" />
                    </SelectTrigger>
                    <SelectContent>
                      {k8sVersionsData?.versions?.map((version) => (
                        <SelectItem
                          key={version.version}
                          value={version.version}
                          disabled={version.deprecated}
                        >
                          <span className="flex items-center gap-2">
                            {version.version}
                            {version.isDefault && (
                              <Badge variant="secondary" className="text-xs">
                                Default
                              </Badge>
                            )}
                            {version.deprecated && (
                              <Badge variant="destructive" className="text-xs">
                                Deprecated
                              </Badge>
                            )}
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </div>

              {/* Node Pools */}
              <div className="space-y-4">
                <div className="flex items-center justify-between">
                  <div>
                    <Label>Node Pools</Label>
                    <p className="text-xs text-muted-foreground">
                      Configure worker node pools for your cluster
                    </p>
                  </div>
                  <Button variant="outline" size="sm" onClick={addNodePool}>
                    <Plus className="mr-2 h-4 w-4" />
                    Add Pool
                  </Button>
                </div>

                {nodePools.map((pool, index) => (
                  <Card key={index} className="bg-muted/50">
                    <CardContent className="pt-4">
                      <div className="flex items-center justify-between mb-4">
                        <div className="flex items-center gap-2">
                          <Server className="h-4 w-4" />
                          <Input
                            value={pool.name}
                            onChange={(e) =>
                              updateNodePool(index, { name: e.target.value })
                            }
                            className="w-40 h-8"
                            placeholder="Pool name"
                          />
                        </div>
                        {nodePools.length > 1 && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => removeNodePool(index)}
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        )}
                      </div>
                      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                        <div className="space-y-2">
                          <Label>Machine Type</Label>
                          {machineTypesLoading ? (
                            <LoadingState message="Loading..." />
                          ) : (
                            <Select
                              value={pool.machineType}
                              onValueChange={(value) =>
                                updateNodePool(index, { machineType: value })
                              }
                            >
                              <SelectTrigger>
                                <SelectValue placeholder="Select" />
                              </SelectTrigger>
                              <SelectContent>
                                {machineTypesData?.machineTypes?.map((type) => (
                                  <SelectItem key={type.name} value={type.name}>
                                    <span className="flex items-center gap-2">
                                      {type.name}
                                      <span className="text-xs text-muted-foreground">
                                        ({type.vcpus} vCPU, {type.memoryGb}GB)
                                      </span>
                                    </span>
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          )}
                        </div>
                        <div className="space-y-2">
                          <Label>Min Nodes</Label>
                          <Input
                            type="number"
                            min={0}
                            value={pool.minNodes}
                            onChange={(e) =>
                              updateNodePool(index, {
                                minNodes: parseInt(e.target.value) || 0,
                              })
                            }
                          />
                        </div>
                        <div className="space-y-2">
                          <Label>Max Nodes</Label>
                          <Input
                            type="number"
                            min={1}
                            value={pool.maxNodes}
                            onChange={(e) =>
                              updateNodePool(index, {
                                maxNodes: parseInt(e.target.value) || 1,
                              })
                            }
                          />
                        </div>
                        <div className="space-y-2">
                          <Label>Disk Size (GB)</Label>
                          <Input
                            type="number"
                            min={20}
                            value={pool.diskSizeGb}
                            onChange={(e) =>
                              updateNodePool(index, {
                                diskSizeGb: parseInt(e.target.value) || 100,
                              })
                            }
                          />
                        </div>
                      </div>
                      <div className="mt-4 flex items-center gap-4">
                        <div className="flex items-center gap-2">
                          <input
                            type="checkbox"
                            id={`preemptible-${index}`}
                            checked={pool.preemptible}
                            onChange={(e) =>
                              updateNodePool(index, { preemptible: e.target.checked })
                            }
                            className="h-4 w-4 rounded border-gray-300 text-primary focus:ring-primary"
                          />
                          <Label htmlFor={`preemptible-${index}`} className="text-sm">
                            Preemptible/Spot instances (cheaper, may be interrupted)
                          </Label>
                        </div>
                      </div>
                    </CardContent>
                  </Card>
                ))}
              </div>

              {/* Networking */}
              <div className="space-y-4">
                <div>
                  <Label>Networking</Label>
                  <p className="text-xs text-muted-foreground">
                    Configure VPC and cluster networking
                  </p>
                </div>
                <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
                  <div className="space-y-2">
                    <Label htmlFor="vpc-cidr">VPC CIDR</Label>
                    <Input
                      id="vpc-cidr"
                      value={networking.vpcCidr}
                      onChange={(e) =>
                        setNetworking({ ...networking, vpcCidr: e.target.value })
                      }
                      placeholder="10.0.0.0/16"
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="pod-cidr">Pod CIDR</Label>
                    <Input
                      id="pod-cidr"
                      value={networking.podCidr}
                      onChange={(e) =>
                        setNetworking({ ...networking, podCidr: e.target.value })
                      }
                      placeholder="10.244.0.0/16"
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="service-cidr">Service CIDR</Label>
                    <Input
                      id="service-cidr"
                      value={networking.serviceCidr}
                      onChange={(e) =>
                        setNetworking({ ...networking, serviceCidr: e.target.value })
                      }
                      placeholder="10.96.0.0/12"
                    />
                  </div>
                </div>
                <div className="flex items-center gap-6">
                  <div className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      id="private-cluster"
                      checked={networking.privateCluster}
                      onChange={(e) =>
                        setNetworking({ ...networking, privateCluster: e.target.checked })
                      }
                      className="h-4 w-4 rounded border-gray-300 text-primary focus:ring-primary"
                    />
                    <Label htmlFor="private-cluster">Private Cluster</Label>
                  </div>
                  <div className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      id="nat-gateway"
                      checked={networking.natGateway}
                      onChange={(e) =>
                        setNetworking({ ...networking, natGateway: e.target.checked })
                      }
                      className="h-4 w-4 rounded border-gray-300 text-primary focus:ring-primary"
                    />
                    <Label htmlFor="nat-gateway">NAT Gateway</Label>
                  </div>
                </div>
              </div>
            </CardContent>
          </>
        )}

        {currentStep === "review" && provisioningRequest && (
          <>
            <CardHeader>
              <CardTitle>Review Configuration</CardTitle>
              <CardDescription>
                Review the Terraform configuration that will be applied
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              {/* Summary */}
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4 p-4 bg-muted/50 rounded-lg">
                <div>
                  <p className="text-xs text-muted-foreground">Cluster Name</p>
                  <p className="font-medium">{provisioningRequest.name}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Provider</p>
                  <p className="font-medium">{getProviderLabel(provisioningRequest.provider)}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Region</p>
                  <p className="font-medium">{provisioningRequest.region}</p>
                </div>
                <div>
                  <p className="text-xs text-muted-foreground">Kubernetes</p>
                  <p className="font-medium">{provisioningRequest.k8sVersion}</p>
                </div>
              </div>

              {/* Node Pools Summary */}
              <div className="space-y-2">
                <Label>Node Pools</Label>
                <div className="space-y-2">
                  {provisioningRequest.nodePools?.map((pool, index) => (
                    <div
                      key={index}
                      className="flex items-center justify-between p-3 border rounded-lg"
                    >
                      <div className="flex items-center gap-3">
                        <Server className="h-4 w-4 text-muted-foreground" />
                        <span className="font-medium">{pool.name}</span>
                      </div>
                      <div className="text-sm text-muted-foreground">
                        {pool.machineType} • {pool.minNodes}-{pool.maxNodes} nodes •{" "}
                        {pool.diskSizeGb}GB disk
                      </div>
                    </div>
                  ))}
                </div>
              </div>

              {/* Terraform Config */}
              <div className="space-y-2">
                <Label>Generated Terraform Configuration</Label>
                {provisioningRequest.terraformConfig ? (
                  <pre className="p-4 bg-slate-950 text-slate-50 rounded-lg overflow-auto max-h-[400px] text-xs font-mono">
                    {provisioningRequest.terraformConfig}
                  </pre>
                ) : (
                  <div className="p-4 border rounded-lg text-center text-muted-foreground">
                    Terraform configuration not yet generated
                  </div>
                )}
              </div>
            </CardContent>
          </>
        )}

        {currentStep === "confirm" && provisioningRequest && (
          <>
            <CardHeader>
              <CardTitle>Confirm & Start Provisioning</CardTitle>
              <CardDescription>
                Review the final details before creating your cluster
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              {/* Warning */}
              <div className="p-4 bg-yellow-50 border border-yellow-200 rounded-lg flex items-start gap-3 dark:bg-yellow-950 dark:border-yellow-800">
                <AlertTriangle className="h-5 w-5 text-yellow-600 mt-0.5" />
                <div>
                  <p className="font-medium text-yellow-800 dark:text-yellow-200">
                    This will create real cloud infrastructure
                  </p>
                  <p className="text-sm text-yellow-700 dark:text-yellow-300">
                    Creating this cluster will provision actual resources in your cloud account
                    and may incur costs. Make sure you have reviewed the configuration above.
                  </p>
                </div>
              </div>

              {/* Final Summary */}
              <div className="space-y-4">
                <div className="p-4 border rounded-lg space-y-3">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Cluster Name</span>
                    <span className="font-medium">{provisioningRequest.name}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Provider</span>
                    <span className="font-medium">
                      {getProviderLabel(provisioningRequest.provider)}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Region</span>
                    <span className="font-medium">{provisioningRequest.region}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Kubernetes Version</span>
                    <span className="font-medium">{provisioningRequest.k8sVersion}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">Node Pools</span>
                    <span className="font-medium">
                      {provisioningRequest.nodePools?.length || 0} pool(s)
                    </span>
                  </div>
                </div>
              </div>
            </CardContent>
          </>
        )}

        <CardFooter className="flex justify-between">
          <Button
            variant="outline"
            onClick={handleBack}
            disabled={currentStep === "provider" || isLoading}
          >
            <ArrowLeft className="mr-2 h-4 w-4" />
            Back
          </Button>
          <Button onClick={handleNext} disabled={isLoading}>
            {isLoading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {currentStep === "confirm" ? (
              <>
                Start Provisioning
                <ChevronRight className="ml-2 h-4 w-4" />
              </>
            ) : (
              <>
                Next
                <ArrowRight className="ml-2 h-4 w-4" />
              </>
            )}
          </Button>
        </CardFooter>
      </Card>
    </div>
  );
}
