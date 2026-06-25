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
import { FormInput, FormTextarea } from "@/components/shared/form-field";
import { useCreateProject } from "@/hooks/use-projects";
import { slugify } from "@/lib/utils";

const createProjectSchema = z.object({
  name: z.string().min(2, "Name must be at least 2 characters"),
  slug: z
    .string()
    .min(2, "Slug must be at least 2 characters")
    .regex(/^[a-z0-9-]+$/, "Slug can only contain lowercase letters, numbers, and hyphens"),
  description: z.string().optional(),
});

type CreateProjectForm = z.infer<typeof createProjectSchema>;

interface CreateProjectDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  orgId: string;
  orgSlug: string;
}

export function CreateProjectDialog({
  open,
  onOpenChange,
  orgId,
  orgSlug,
}: CreateProjectDialogProps) {
  const router = useRouter();
  const createProject = useCreateProject(orgId);

  const methods = useForm<CreateProjectForm>({
    resolver: zodResolver(createProjectSchema),
    defaultValues: {
      name: "",
      slug: "",
      description: "",
    },
  });

  const { handleSubmit, watch, setValue, reset, formState: { isSubmitting } } = methods;

  const name = watch("name");
  React.useEffect(() => {
    if (name) {
      setValue("slug", slugify(name));
    }
  }, [name, setValue]);

  const onSubmit = async (data: CreateProjectForm) => {
    const project = await createProject.mutateAsync(data);
    reset();
    onOpenChange(false);
    router.push(`/${orgSlug}/projects/${project.slug}`);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create Project</DialogTitle>
          <DialogDescription>
            Create a new project to organize your applications and secrets
          </DialogDescription>
        </DialogHeader>
        <FormProvider {...methods}>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <FormInput<CreateProjectForm>
              name="name"
              label="Project Name"
              placeholder="My Application"
              required
            />
            <FormInput<CreateProjectForm>
              name="slug"
              label="URL Slug"
              description="Used in URLs and resource naming"
              required
            />
            <FormTextarea<CreateProjectForm>
              name="description"
              label="Description"
              placeholder="What is this project for?"
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" loading={isSubmitting}>
                Create Project
              </Button>
            </DialogFooter>
          </form>
        </FormProvider>
      </DialogContent>
    </Dialog>
  );
}
