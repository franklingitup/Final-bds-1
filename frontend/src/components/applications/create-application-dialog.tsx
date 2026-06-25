"use client";

import * as React from "react";
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
import { useCreateApplication } from "@/hooks/use-applications";
import { slugify } from "@/lib/utils";

const createAppSchema = z.object({
  name: z.string().min(2, "Name must be at least 2 characters"),
  slug: z
    .string()
    .min(2, "Slug must be at least 2 characters")
    .regex(/^[a-z0-9-]+$/, "Slug can only contain lowercase letters, numbers, and hyphens"),
  description: z.string().optional(),
  runtimeType: z.string().default("container"),
});

type CreateAppForm = z.infer<typeof createAppSchema>;

interface CreateApplicationDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  orgId: string;
  projectId: string;
}

export function CreateApplicationDialog({
  open,
  onOpenChange,
  orgId,
  projectId,
}: CreateApplicationDialogProps) {
  const createApp = useCreateApplication(orgId, projectId);

  const methods = useForm<CreateAppForm>({
    resolver: zodResolver(createAppSchema),
    defaultValues: {
      name: "",
      slug: "",
      description: "",
      runtimeType: "container",
    },
  });

  const { handleSubmit, watch, setValue, reset, formState: { isSubmitting } } = methods;

  const name = watch("name");
  React.useEffect(() => {
    if (name) {
      setValue("slug", slugify(name));
    }
  }, [name, setValue]);

  const onSubmit = async (data: CreateAppForm) => {
    await createApp.mutateAsync(data);
    reset();
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create Application</DialogTitle>
          <DialogDescription>
            Create a new application to deploy to your clusters
          </DialogDescription>
        </DialogHeader>
        <FormProvider {...methods}>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <FormInput<CreateAppForm>
              name="name"
              label="Application Name"
              placeholder="nginx"
              required
            />
            <FormInput<CreateAppForm>
              name="slug"
              label="URL Slug"
              description="Used in Kubernetes resource naming"
              required
            />
            <FormSelect<CreateAppForm>
              name="runtimeType"
              label="Runtime Type"
              options={[
                { value: "container", label: "Container" },
                { value: "function", label: "Function" },
                { value: "job", label: "Job" },
              ]}
              required
            />
            <FormTextarea<CreateAppForm>
              name="description"
              label="Description"
              placeholder="What does this application do?"
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" loading={isSubmitting}>
                Create Application
              </Button>
            </DialogFooter>
          </form>
        </FormProvider>
      </DialogContent>
    </Dialog>
  );
}
