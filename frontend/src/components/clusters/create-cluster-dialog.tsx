"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useForm, FormProvider } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { FormInput, FormTextarea, FormSelect } from "@/components/shared/form-field";
import { useCreateCluster } from "@/hooks/use-clusters";
import { slugify } from "@/lib/utils";

const createClusterSchema = z.object({
  name: z.string().min(2, "Name must be at least 2 characters"),
  slug: z
    .string()
    .min(2, "Slug must be at least 2 characters")
    .regex(/^[a-z0-9-]+$/, "Slug can only contain lowercase letters, numbers, and hyphens"),
  description: z.string().optional(),
  cloudProvider: z.string().optional(),
  region: z.string().optional(),
});

type CreateClusterForm = z.infer<typeof createClusterSchema>;

interface CreateClusterDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  orgId: string;
  orgSlug: string;
}

export function CreateClusterDialog({
  open,
  onOpenChange,
  orgId,
  orgSlug,
}: CreateClusterDialogProps) {
  const router = useRouter();
  const createCluster = useCreateCluster(orgId);

  const methods = useForm<CreateClusterForm>({
    resolver: zodResolver(createClusterSchema),
    defaultValues: {
      name: "",
      slug: "",
      description: "",
      cloudProvider: "",
      region: "",
    },
  });

  const { handleSubmit, watch, setValue, reset, formState: { isSubmitting } } = methods;

  const name = watch("name");
  React.useEffect(() => {
    if (name) {
      setValue("slug", slugify(name));
    }
  }, [name, setValue]);

  const onSubmit = async (data: CreateClusterForm) => {
    const cluster = await createCluster.mutateAsync(data);
    reset();
    onOpenChange(false);
    router.push(`/${orgSlug}/clusters/${cluster.id}`);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add Cluster</DialogTitle>
          <DialogDescription>
            Register a new Kubernetes cluster to deploy applications
          </DialogDescription>
        </DialogHeader>
        <FormProvider {...methods}>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <FormInput<CreateClusterForm>
              name="name"
              label="Cluster Name"
              placeholder="production-us-west"
              required
            />
            <FormInput<CreateClusterForm>
              name="slug"
              label="URL Slug"
              description="Used in URLs and resource naming"
              required
            />
            <div className="grid grid-cols-2 gap-4">
              <FormSelect<CreateClusterForm>
                name="cloudProvider"
                label="Cloud Provider"
                placeholder="Select provider"
                options={[
                  { value: "aws", label: "AWS" },
                  { value: "gcp", label: "GCP" },
                  { value: "azure", label: "Azure" },
                  { value: "other", label: "Other" },
                ]}
              />
              <FormInput<CreateClusterForm>
                name="region"
                label="Region"
                placeholder="us-west-2"
              />
            </div>
            <FormTextarea<CreateClusterForm>
              name="description"
              label="Description"
              placeholder="Production cluster in US West"
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" loading={isSubmitting}>
                Create Cluster
              </Button>
            </DialogFooter>
          </form>
        </FormProvider>
      </DialogContent>
    </Dialog>
  );
}
