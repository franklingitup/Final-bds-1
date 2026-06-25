"use client";

import * as React from "react";
import { useRouter, useParams } from "next/navigation";
import { useForm, FormProvider, useFieldArray } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Plus, X } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { FormInput, FormSelect } from "@/components/shared/form-field";
import { useCreateDeployment } from "@/hooks/use-deployments";
import { useApplications } from "@/hooks/use-applications";
import { useClusters } from "@/hooks/use-clusters";
import { useProjects } from "@/hooks/use-projects";

const envVarSchema = z.object({
  name: z.string().min(1),
  value: z.string().min(1),
});

const createDeploymentSchema = z.object({
  projectId: z.string().min(1, "Project is required"),
  applicationId: z.string().min(1, "Application is required"),
  clusterId: z.string().min(1, "Cluster is required"),
  image: z.string().min(1, "Image is required"),
  replicas: z.coerce.number().min(1, "At least 1 replica required").max(100),
  port: z.coerce.number().optional(),
  cpuRequest: z.string().optional(),
  cpuLimit: z.string().optional(),
  memoryRequest: z.string().optional(),
  memoryLimit: z.string().optional(),
  envVars: z.array(envVarSchema).default([]),
});

type CreateDeploymentForm = z.infer<typeof createDeploymentSchema>;

interface CreateDeploymentDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  orgId: string;
}

export function CreateDeploymentDialog({
  open,
  onOpenChange,
  orgId,
}: CreateDeploymentDialogProps) {
  const router = useRouter();
  const params = useParams();
  const orgSlug = params.orgSlug as string;

  const createDeployment = useCreateDeployment(orgId);
  const { data: projects } = useProjects(orgId);
  const { data: clusters } = useClusters(orgId, "connected");

  const methods = useForm<CreateDeploymentForm>({
    resolver: zodResolver(createDeploymentSchema),
    defaultValues: {
      projectId: "",
      applicationId: "",
      clusterId: "",
      image: "",
      replicas: 1,
      port: undefined,
      cpuRequest: "100m",
      cpuLimit: "500m",
      memoryRequest: "128Mi",
      memoryLimit: "512Mi",
      envVars: [],
    },
  });

  const { handleSubmit, watch, control, reset, formState: { isSubmitting } } = methods;
  const { fields, append, remove } = useFieldArray({ control, name: "envVars" });

  const selectedProjectId = watch("projectId");
  const { data: apps } = useApplications(orgId, selectedProjectId);

  const onSubmit = async (data: CreateDeploymentForm) => {
    const deployment = await createDeployment.mutateAsync({
      applicationId: data.applicationId,
      clusterId: data.clusterId,
      image: data.image,
      replicas: data.replicas,
      port: data.port,
      cpuRequest: data.cpuRequest,
      cpuLimit: data.cpuLimit,
      memoryRequest: data.memoryRequest,
      memoryLimit: data.memoryLimit,
      envVars: data.envVars,
    });
    reset();
    onOpenChange(false);
    router.push(`/${orgSlug}/deployments/${deployment.id}`);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Create Deployment</DialogTitle>
          <DialogDescription>
            Deploy an application to a cluster
          </DialogDescription>
        </DialogHeader>
        <FormProvider {...methods}>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
            <div className="grid grid-cols-2 gap-4">
              <FormSelect<CreateDeploymentForm>
                name="projectId"
                label="Project"
                placeholder="Select project"
                options={projects?.items.map((p) => ({ value: p.id, label: p.name })) || []}
                required
              />
              <FormSelect<CreateDeploymentForm>
                name="applicationId"
                label="Application"
                placeholder="Select application"
                options={apps?.items.map((a) => ({ value: a.id, label: a.name })) || []}
                required
              />
            </div>

            <FormSelect<CreateDeploymentForm>
              name="clusterId"
              label="Cluster"
              placeholder="Select cluster"
              options={clusters?.items.map((c) => ({ value: c.id, label: c.name })) || []}
              required
            />

            <div className="grid grid-cols-2 gap-4">
              <FormInput<CreateDeploymentForm>
                name="image"
                label="Docker Image"
                placeholder="nginx:latest"
                required
              />
              <FormInput<CreateDeploymentForm>
                name="replicas"
                label="Replicas"
                type="number"
                required
              />
            </div>

            <FormInput<CreateDeploymentForm>
              name="port"
              label="Container Port"
              type="number"
              placeholder="80"
              description="Port to expose on the container"
            />

            <div className="space-y-2">
              <Label>Resource Limits</Label>
              <div className="grid grid-cols-2 gap-4">
                <FormInput<CreateDeploymentForm>
                  name="cpuRequest"
                  label="CPU Request"
                  placeholder="100m"
                />
                <FormInput<CreateDeploymentForm>
                  name="cpuLimit"
                  label="CPU Limit"
                  placeholder="500m"
                />
                <FormInput<CreateDeploymentForm>
                  name="memoryRequest"
                  label="Memory Request"
                  placeholder="128Mi"
                />
                <FormInput<CreateDeploymentForm>
                  name="memoryLimit"
                  label="Memory Limit"
                  placeholder="512Mi"
                />
              </div>
            </div>

            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <Label>Environment Variables</Label>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => append({ name: "", value: "" })}
                >
                  <Plus className="mr-2 h-4 w-4" />
                  Add Variable
                </Button>
              </div>
              <div className="space-y-2">
                {fields.map((field, index) => (
                  <div key={field.id} className="flex gap-2">
                    <Input
                      {...methods.register(`envVars.${index}.name`)}
                      placeholder="NAME"
                      className="flex-1"
                    />
                    <Input
                      {...methods.register(`envVars.${index}.value`)}
                      placeholder="value"
                      className="flex-1"
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      onClick={() => remove(index)}
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  </div>
                ))}
              </div>
            </div>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" loading={isSubmitting}>
                Deploy
              </Button>
            </DialogFooter>
          </form>
        </FormProvider>
      </DialogContent>
    </Dialog>
  );
}
