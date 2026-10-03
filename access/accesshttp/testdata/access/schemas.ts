import { z } from 'zod';

export const BindInputSchema = z.object({
  id: z.number().int(),
  role_ref: z.string().min(1).max(64),
  level: z.string().min(1).max(32),
  scope_id: z.number().int().nullable().optional(),
});

export type BindInput = z.infer<typeof BindInputSchema>;

export const BindableInputSchema = z.object({
  level: z.string().min(1).max(32),
  scope_id: z.number().int(),
});

export type BindableInput = z.infer<typeof BindableInputSchema>;

export const CompareInputSchema = z.object({
  ref: z.string().min(1).max(64),
  with: z.string().min(1).max(64),
});

export type CompareInput = z.infer<typeof CompareInputSchema>;

export const ConstraintSchema = z.object({
  attribute: z.string().min(1).max(64),
  values: z.array(z.string()),
});

export type Constraint = z.infer<typeof ConstraintSchema>;

export const GrantInputSchema = z.object({
  permission: z.string().min(1).max(100),
  qualifier: z.string().min(1).max(16),
});

export type GrantInput = z.infer<typeof GrantInputSchema>;

export const CreateRoleInputSchema = z.object({
  name: z.string().min(1).max(100),
  description: z.string().max(255),
  grants: z.array(GrantInputSchema),
  attrs: z.array(ConstraintSchema),
});

export type CreateRoleInput = z.infer<typeof CreateRoleInputSchema>;

export const ReplaceRoleInputSchema = z.object({
  ref: z.string().min(1).max(64),
  with: z.string().min(1).max(64),
});

export type ReplaceRoleInput = z.infer<typeof ReplaceRoleInputSchema>;

export const RoleInputSchema = z.object({
  ref: z.string().min(1).max(64),
});

export type RoleInput = z.infer<typeof RoleInputSchema>;

export const SubjectInputSchema = z.object({
  id: z.number().int(),
});

export type SubjectInput = z.infer<typeof SubjectInputSchema>;

export const UnbindInputSchema = z.object({
  id: z.number().int(),
  binding_id: z.number().int(),
});

export type UnbindInput = z.infer<typeof UnbindInputSchema>;

export const UpdateRoleInputSchema = z.object({
  ref: z.string().min(1).max(64),
  name: z.string().min(1).max(100),
  description: z.string().max(255),
  grants: z.array(GrantInputSchema),
  attrs: z.array(ConstraintSchema),
});

export type UpdateRoleInput = z.infer<typeof UpdateRoleInputSchema>;
