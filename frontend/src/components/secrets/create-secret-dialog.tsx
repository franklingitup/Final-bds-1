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
import { FormInput, FormTextarea } from "@/components/shared/form-field";
import { useCreateSecret } from "@/hooks/use-secrets";

const createSecretSchema = z.object({
  name: z
    .string()
    .min(1, "Name is required")
    .regex(/^[A-Z_][A-Z0-9_]*$/, "Name must be uppercase with underscores (e.g., DATABASE_URL)"),
  value: z.string().min(1, "Value is required"),
  description: z.string().optional(),
});

type CreateSecretForm = z.infer<typeof createSecretSchema>;

interface CreateSecretDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  orgId: string;
  projectId: string;
}

export function CreateSecretDialog({
  open,
  onOpenChange,
  orgId,
  projectId,
}: CreateSecretDialogProps) {
  const createSecret = useCreateSecret(orgId, projectId);

  const methods = useForm<CreateSecretForm>({
    resolver: zodResolver(createSecretSchema),
    defaultValues: {
      name: "",
      value: "",
      description: "",
    },
  });

  const { handleSubmit, reset, formState: { isSubmitting } } = methods;

  const onSubmit = async (data: CreateSecretForm) => {
    await createSecret.mutateAsync(data);
    reset();
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create Secret</DialogTitle>
          <DialogDescription>
            Secrets are encrypted and will never be displayed after creation
          </DialogDescription>
        </DialogHeader>
        <FormProvider {...methods}>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <FormInput<CreateSecretForm>
              name="name"
              label="Secret Name"
              placeholder="DATABASE_URL"
              description="Must be uppercase with underscores"
              required
            />
            <FormTextarea<CreateSecretForm>
              name="value"
              label="Secret Value"
              placeholder="postgres://user:password@localhost:5432/db"
              required
            />
            <FormTextarea<CreateSecretForm>
              name="description"
              label="Description"
              placeholder="Primary database connection string"
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" loading={isSubmitting}>
                Create Secret
              </Button>
            </DialogFooter>
          </form>
        </FormProvider>
      </DialogContent>
    </Dialog>
  );
}
