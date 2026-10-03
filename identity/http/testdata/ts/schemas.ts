import { z } from 'zod';

export const ChangeLocaleInputSchema = z.object({
  locale: z.string().min(1).max(16),
});

export type ChangeLocaleInput = z.infer<typeof ChangeLocaleInputSchema>;

export const LoginAsInputSchema = z.object({
  user_id: z.number().int().refine((n) => n !== 0, { params: { code: 'required' } }),
});

export type LoginAsInput = z.infer<typeof LoginAsInputSchema>;

export const LoginInputSchema = z.object({
  login: z.string().min(1).max(100),
  password: z.string().min(1).max(200),
});

export type LoginInput = z.infer<typeof LoginInputSchema>;

export const RefreshInputSchema = z.object({
  refresh_token: z.string().max(255),
});

export type RefreshInput = z.infer<typeof RefreshInputSchema>;
