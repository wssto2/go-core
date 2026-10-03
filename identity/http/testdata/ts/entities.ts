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

export type SessionResponse = {
  user: any;
  expires_at: string;
  access: MyAccess;
  navigation?: Node[];
};

export type Subject = {
  kind: string;
  id: number | null;
};

export type User = {
  id: number | null;
  login: string;
  name: string;
  email: string;
  locale: string;
};
