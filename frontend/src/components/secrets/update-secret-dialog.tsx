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
import { FormTextarea } from "@/components/shared/form-field";
import { useUpdateSecret } from "@/hooks/use-secrets";
import type { Secret } from "@/types/api";

const updateSecretSchema = z.object({
  value: z.string().min(1, "Value is required"),
  description: z.string().optional(),
});

type UpdateSecretForm = z.infer<typeof updateSecretSchema>;

interface UpdateSecretDialogProps {
  secret: Secret | null;
  onClose: () => void;
  orgId: string;
  projectId: string;
}

export function UpdateSecretDialog({
  secret,
  onClose,
  orgId,
  projectId,
}: UpdateSecretDialogProps) {
  const updateSecret = useUpdateSecret(orgId, projectId, secret?.id || "");

  const methods = useForm<UpdateSecretForm>({
    resolver: zodResolver(updateSecretSchema),
    defaultValues: {
      value: "",
      description: secret?.description || "",
    },
  });

  const { handleSubmit, reset, formState: { isSubmitting } } = methods;

  React.useEffect(() => {
    if (secret) {
      reset({ value: "", description: secret.description || "" });
    }
  }, [secret, reset]);

  const onSubmit = async (data: UpdateSecretForm) => {
    await updateSecret.mutateAsync(data);
    reset();
    onClose();
  };

  return (
    <Dialog open={!!secret} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Update Secret: {secret?.name}</DialogTitle>
          <DialogDescription>
            Enter a new value for this secret. The current value cannot be displayed.
          </DialogDescription>
        </DialogHeader>
        <FormProvider {...methods}>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <FormTextarea<UpdateSecretForm>
              name="value"
              label="New Value"
              placeholder="Enter new secret value"
              required
            />
            <FormTextarea<UpdateSecretForm>
              name="description"
              label="Description"
              placeholder="Description of this secret"
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" loading={isSubmitting}>
                Update Secret
              </Button>
            </DialogFooter>
          </form>
        </FormProvider>
      </DialogContent>
    </Dialog>
  );
}
