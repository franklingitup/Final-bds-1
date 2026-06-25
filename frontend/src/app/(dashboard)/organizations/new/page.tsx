"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useForm, FormProvider } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { FormInput, FormTextarea } from "@/components/shared/form-field";
import { useCreateOrganization } from "@/hooks/use-organizations";
import { slugify } from "@/lib/utils";

const createOrgSchema = z.object({
  name: z.string().min(2, "Name must be at least 2 characters"),
  slug: z
    .string()
    .min(2, "Slug must be at least 2 characters")
    .regex(/^[a-z0-9-]+$/, "Slug can only contain lowercase letters, numbers, and hyphens"),
  description: z.string().optional(),
});

type CreateOrgForm = z.infer<typeof createOrgSchema>;

export default function NewOrganizationPage() {
  const router = useRouter();
  const createOrg = useCreateOrganization();

  const methods = useForm<CreateOrgForm>({
    resolver: zodResolver(createOrgSchema),
    defaultValues: {
      name: "",
      slug: "",
      description: "",
    },
  });

  const { handleSubmit, watch, setValue, formState: { isSubmitting } } = methods;

  const name = watch("name");
  React.useEffect(() => {
    if (name) {
      setValue("slug", slugify(name));
    }
  }, [name, setValue]);

  const onSubmit = async (data: CreateOrgForm) => {
    const org = await createOrg.mutateAsync(data);
    router.push(`/${org.slug}/projects`);
  };

  return (
    <div className="container max-w-lg py-12">
      <Card>
        <CardHeader>
          <CardTitle>Create Organization</CardTitle>
          <CardDescription>
            Create a new organization to collaborate with your team
          </CardDescription>
        </CardHeader>
        <FormProvider {...methods}>
          <form onSubmit={handleSubmit(onSubmit)}>
            <CardContent className="space-y-4">
              <FormInput<CreateOrgForm>
                name="name"
                label="Organization Name"
                placeholder="My Company"
                required
              />
              <FormInput<CreateOrgForm>
                name="slug"
                label="URL Slug"
                description="This will be used in URLs: bdsplatform.io/my-company"
                required
              />
              <FormTextarea<CreateOrgForm>
                name="description"
                label="Description"
                placeholder="What does your organization do?"
              />
            </CardContent>
            <CardFooter>
              <Button type="submit" className="w-full" loading={isSubmitting}>
                Create Organization
              </Button>
            </CardFooter>
          </form>
        </FormProvider>
      </Card>
    </div>
  );
}
