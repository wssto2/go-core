import { z } from 'zod';

export const ChangeLocaleInputSchema = z.object({
  locale: z.string().min(1).max(16),
});

export type ChangeLocaleInput = z.infer<typeof ChangeLocaleInputSchema>;

export const ChangePasswordInputSchema = z.object({
  current_password: z.string().min(1).max(200),
  new_password: z.string().min(1).max(200),
  new_password_confirmation: z.string().min(1).max(200),
});

export type ChangePasswordInput = z.infer<typeof ChangePasswordInputSchema>;

export const ConfirmEmailInputSchema = z.object({
  code: z.string().min(1).max(12),
});

export type ConfirmEmailInput = z.infer<typeof ConfirmEmailInputSchema>;

export const CreateUserInputSchema = z.object({
  login: z.string().min(1).max(100),
  name: z.string().min(1).max(150),
  email: z.string().min(1).max(255),
  phone: z.string().max(30).optional(),
  locale: z.string().min(1).max(16),
  password: z.string().min(1).max(200),
});

export type CreateUserInput = z.infer<typeof CreateUserInputSchema>;

export const HistoryInputSchema = z.object({
  id: z.number().int(),
  page: z.number().int().optional(),
  per_page: z.number().int().optional(),
});

export type HistoryInput = z.infer<typeof HistoryInputSchema>;

export const ListUsersInputSchema = z.object({
  view: z.string().max(16).optional(),
  search: z.string().max(100).optional(),
  order_col: z.string().max(16).optional(),
  order_dir: z.string().max(4).optional(),
  page: z.number().int().optional(),
  per_page: z.number().int().optional(),
});

export type ListUsersInput = z.infer<typeof ListUsersInputSchema>;

export const LoginAsInputSchema = z.object({
  user_id: z.number().int().refine((n) => n !== 0, { params: { code: 'required' } }),
});

export type LoginAsInput = z.infer<typeof LoginAsInputSchema>;

export const LoginInputSchema = z.object({
  login: z.string().min(1).max(100),
  password: z.string().min(1).max(200),
});

export type LoginInput = z.infer<typeof LoginInputSchema>;

export const OwnSessionInputSchema = z.object({
  session_id: z.number().int(),
});

export type OwnSessionInput = z.infer<typeof OwnSessionInputSchema>;

export const PageInputSchema = z.object({
  page: z.number().int().optional(),
  per_page: z.number().int().optional(),
});

export type PageInput = z.infer<typeof PageInputSchema>;

export const RefreshInputSchema = z.object({
  refresh_token: z.string().max(255),
});

export type RefreshInput = z.infer<typeof RefreshInputSchema>;

export const RequestEmailInputSchema = z.object({
  email: z.string().min(1).max(255),
  current_password: z.string().min(1).max(200),
});

export type RequestEmailInput = z.infer<typeof RequestEmailInputSchema>;

export const SetPasswordInputSchema = z.object({
  id: z.number().int(),
  password: z.string().min(1).max(200),
});

export type SetPasswordInput = z.infer<typeof SetPasswordInputSchema>;

export const UpdateProfileInputSchema = z.object({
  name: z.string().min(1).max(150),
  phone: z.string().max(30).optional(),
});

export type UpdateProfileInput = z.infer<typeof UpdateProfileInputSchema>;

export const UpdateUserInputSchema = z.object({
  id: z.number().int(),
  login: z.string().min(1).max(100),
  name: z.string().min(1).max(150),
  email: z.string().min(1).max(255),
  phone: z.string().max(30).optional(),
  locale: z.string().min(1).max(16),
});

export type UpdateUserInput = z.infer<typeof UpdateUserInputSchema>;

export const UserInputSchema = z.object({
  id: z.number().int(),
});

export type UserInput = z.infer<typeof UserInputSchema>;

export const UserSessionInputSchema = z.object({
  id: z.number().int(),
  session_id: z.number().int(),
});

export type UserSessionInput = z.infer<typeof UserSessionInputSchema>;
