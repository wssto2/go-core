export type BindableRoles = {
  roles: Role[];
};

export type Binding = {
  id: number;
  role: RoleSummary;
  scope: Scope;
  created_by: PersonRef | null;
  created_at: string;
};

export type Constraint = {
  attribute: string;
  values: string[];
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

export type GrantDifference = {
  permission: string;
  role: string;
  other: string;
};

export type Kind = "user" | "service";

export type PersonRef = {
  id: number;
  name: string;
};

export type Replaced = {
  rebound: number;
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

export type RoleComparison = {
  only_in_role: Grant[];
  only_in_other: Grant[];
  different: GrantDifference[];
};

export type RoleHolder = {
  subject: SubjectRef;
  name: string;
  scope: Scope;
};

export type RoleHolders = {
  holders: RoleHolder[];
};

export type RoleList = {
  roles: RoleSummary[];
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
  root: boolean;
  places: ScopeOption[];
};

export type SubjectAccess = {
  subject: SubjectRef;
  bindings: Binding[];
  effective: EffectivePermission[];
  can_manage: boolean;
};

export type SubjectRef = {
  kind: Kind;
  id: number;
};
