export type ActivityAction = "created" | "changed" | "deleted";

export type ActivityRow = {
  id: number;
  area: string;
  record_type: string;
  record_id: number;
  action: ActivityAction;
  signed_in_as: PersonRef | null;
  created_at: string;
};

export type ChangeAction = "created" | "updated" | "deactivated" | "activated" | "password" | "email" | "profile";

export type ChangeRow = {
  id: number;
  action: ChangeAction;
  fields: string[];
  before: Record<string, string>;
  after: Record<string, string>;
  actor: PersonRef | null;
  created_at: string;
};

export type ClauseInfo = {
  scope: Scope;
  qualifier: number;
  attrs?: Record<string, string[]>;
  role?: string;
  binding_id?: number;
};

export type Impersonator = {
  id: number;
  name: string;
};

export type MyAccess = {
  subject: Subject;
  root: boolean;
  permissions: Record<string, PermissionAccess>;
  unavailable?: string[];
};

export type Node = {
  i18n: string;
  icon?: string;
  route?: string;
  children?: Node[];
  permissions?: string[];
};

export type PendingEmail = {
  email: string;
  expires_at: string;
  resend_available_at: string;
  attempts_left: number;
};

export type PermissionAccess = {
  scope: Scope;
  qualifier: number;
  clauses: ClauseInfo[];
};

export type PersonRef = {
  id: number;
  name: string;
};

export type ProfileResponse = {
  id: number;
  login: string;
  name: string;
  email: string;
  phone: string;
  locale: string;
  created_at: string;
  pending_email: PendingEmail | null;
};

export type Scope = {
  level: string;
  id?: number;
};

export type SessionItem = {
  id: number;
  device: string;
  ip: string;
  opened_by: PersonRef | null;
  current: boolean;
  last_used_at: string;
  expires_at: string;
  created_at: string;
};

export type SessionList = {
  sessions: SessionItem[];
};

export type SessionResponse = {
  user: unknown;
  expires_at: string;
  impersonator?: Impersonator | null;
  access: MyAccess;
  navigation?: Node[];
};

export type SignInEvent = "signed_in" | "wrong_password" | "locked_out" | "refused_inactive" | "signed_in_as" | "unlocked" | "signed_out_everywhere" | "session_revoked";

export type SignInRow = {
  id: number;
  event: SignInEvent;
  ip: string;
  device: string;
  actor: PersonRef | null;
  created_at: string;
};

export type Status = "active" | "locked" | "inactive";

export type Subject = {
  kind: string;
  id: number;
};

export type UnlockResult = {
  unlocked: boolean;
};

export type User = {
  id: number;
  login: string;
  name: string;
  email: string;
  locale: string;
};

export type UserDetail = {
  id: number;
  login: string;
  name: string;
  email: string;
  phone: string;
  locale: string;
  active: boolean;
  status: Status;
  last_sign_in: string | null;
  locked_until: string | null;
  created_at: string;
};

export type UserRow = {
  id: number;
  login: string;
  name: string;
  email: string;
  phone: string;
  locale: string;
  active: boolean;
  status: Status;
  last_sign_in: string | null;
  locked_until: string | null;
  created_at: string;
};

export type ViewCount = {
  key: string;
  count: number;
};
