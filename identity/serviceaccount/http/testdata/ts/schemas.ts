import { z } from 'zod';

export const AccountPathSchema = z.object({
  id: z.number().int(),
});

export type AccountPath = z.infer<typeof AccountPathSchema>;

export const BindInputSchema = z.object({
  id: z.number().int(),
  role_ref: z.string().trim().min(1).max(64),
  level: z.string().trim().min(1).max(32),
  scope_id: z.number().int().nullable().optional(),
});

export type BindInput = z.infer<typeof BindInputSchema>;

export const BindableInputSchema = z.object({
  level: z.string().trim().min(1).max(32),
  scope_id: z.number().int(),
});

export type BindableInput = z.infer<typeof BindableInputSchema>;

export const KeyRequestSchema = z.object({
  name: z.string().min(1).max(100),
  expires_at: z.string().datetime({ offset: true }).nullable().optional(),
});

export type KeyRequest = z.infer<typeof KeyRequestSchema>;

export const RevokeRequestSchema = z.object({
  keyID: z.number().int(),
});

export type RevokeRequest = z.infer<typeof RevokeRequestSchema>;

export const SaveInputSchema = z.object({
  name: z.string().min(1).max(100),
  description: z.string().max(500),
  tenant_id: z.number().int().nullable().optional(),
  owner_id: z.number().int().min(1).refine((n) => n !== 0, { params: { code: 'required' } }),
});

export type SaveInput = z.infer<typeof SaveInputSchema>;

export const SubjectInputSchema = z.object({
  id: z.number().int(),
});

export type SubjectInput = z.infer<typeof SubjectInputSchema>;

export const UnbindInputSchema = z.object({
  id: z.number().int(),
  binding_id: z.number().int(),
});

export type UnbindInput = z.infer<typeof UnbindInputSchema>;

export const UpdateRequestSchema = z.object({
  name: z.string().min(1).max(100),
  description: z.string().max(500),
  tenant_id: z.number().int().nullable().optional(),
  owner_id: z.number().int().min(1).refine((n) => n !== 0, { params: { code: 'required' } }),
});

export type UpdateRequest = z.infer<typeof UpdateRequestSchema>;
