export type AccountTenant = {
  id: number;
  name: string;
};

export type ApiKey = {
  id: number;
  name: string;
  prefix: string;
  created_by: PersonRef;
  created_at: string;
  last_used_at: string | null;
  expires_at: string | null;
  revoked_at: string | null;
};

export type ApiKeyList = {
  keys: ApiKey[];
};

export type BindableRoles = {
  roles: Role[];
};

export type Binding = {
  id: number;
  role: RoleSummary;
  scope: Scope;
  created_by: BindingPersonRef | null;
  created_at: string;
};

export type BindingPersonRef = {
  id: number;
  name: string;
};

export type Constraint = {
  attribute: string;
  values: string[];
};

export type CreatedApiKey = {
  key: ApiKey;
  secret: string;
};

export type EffectiveGrant = {
  qualifier: string;
  scope: Scope;
  attrs: Constraint[];
  binding_id: number;
  role_key: string;
  role_name: string;
};

export type EffectivePermission = {
  permission: string;
  grants: EffectiveGrant[];
  unavailable: boolean;
};

export type Grant = {
  permission: string;
  qualifier: string;
};

export type PersonRef = {
  id: number;
  name: string;
};

export type Role = {
  ref: string;
  id: number | null;
  key: string | null;
  name: string;
  description: string;
  predefined: boolean;
  computed: boolean;
  attrs: Constraint[];
  holders: number;
  permission_count: number;
  grants: Grant[];
};

export type RoleSummary = {
  ref: string;
  id: number | null;
  key: string | null;
  name: string;
  description: string;
  predefined: boolean;
  computed: boolean;
  attrs: Constraint[];
  holders: number;
  permission_count: number;
};

export type Scope = {
  level: string;
  id: number | null;
  name: string | null;
};

export type ScopeOption = {
  level: string;
  id: number;
  name: string;
  parent_level: string;
  parent_id: number | null;
};

export type ScopeOptions = {
  root_level: string;
  root: boolean;
  places: ScopeOption[];
};

export type ServiceAccount = {
  id: number;
  name: string;
  description: string;
  tenant: AccountTenant | null;
  owner: PersonRef;
  created_by: PersonRef;
  created_at: string;
  active_keys: number;
  last_used_at: string | null;
  deactivated_at: string | null;
};

export type ServiceAccountList = {
  service_accounts: ServiceAccount[];
};

export type SubjectAccess = {
  subject: SubjectRef;
  bindings: Binding[];
  effective: EffectivePermission[];
  can_manage: boolean;
};

export type SubjectRef = {
  kind: string;
  id: number;
};
