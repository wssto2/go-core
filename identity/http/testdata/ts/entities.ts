export type ChangeAction = "created" | "updated" | "deactivated" | "activated" | "password" | "email" | "profile";

export type ChangeRow = {
  id: number | null;
  action: ChangeAction;
  fields: string[];
  before: any;
  after: any;
  actor_id: number | null;
  created_at: string;
};

export type Event = "signed_in" | "wrong_password" | "locked_out" | "refused_inactive" | "signed_in_as" | "unlocked" | "signed_out_everywhere" | "session_revoked";

export type MyAccess = {
  subject: Subject;
  root: boolean;
  permissions: any;
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

export type ProfileResponse = {
  id: number | null;
  login: string;
  name: string;
  email: string;
  phone: string;
  locale: string;
  created_at: string;
  pending_email: PendingEmail | null;
};

export type SessionItem = {
  id: number | null;
  device: string;
  ip: string;
  opened_by: number | null;
  current: boolean;
  last_used_at: string;
  expires_at: string;
  created_at: string;
};

export type SessionList = {
  sessions: SessionItem[];
};

export type SessionResponse = {
  user: any;
  expires_at: string;
  access: MyAccess;
  navigation?: Node[];
};

export type SignInRow = {
  id: number | null;
  event: Event;
  ip: string;
  device: string;
  actor_id: number | null;
  created_at: string;
};

export type Status = "active" | "locked" | "inactive";

export type Subject = {
  kind: string;
  id: number | null;
};

export type UnlockResult = {
  unlocked: boolean;
};

export type User = {
  id: number | null;
  login: string;
  name: string;
  email: string;
  locale: string;
};

export type UserDetail = {
  id: number | null;
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
  id: number | null;
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
