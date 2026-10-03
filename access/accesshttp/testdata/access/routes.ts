import { route } from "@wssto2/vue-core";
import type { BindInput, BindableInput, CompareInput, CreateRoleInput, ReplaceRoleInput, RoleInput, SubjectInput, UnbindInput, UpdateRoleInput } from "./schemas";
import type { BindableRoles, Binding, Replaced, Role, RoleComparison, RoleHolders, RoleList, ScopeOptions, SubjectAccess } from "./entities";

export const accessRoutes = {
  rolesList: route<void, RoleList>("GET", "/v1/iam/roles", { permission: "iam.role:view" }),
  rolesShow: route<RoleInput, Role>("GET", "/v1/iam/roles/:ref", { permission: "iam.role:view" }),
  rolesHolders: route<RoleInput, RoleHolders>("GET", "/v1/iam/roles/:ref/holders", { permission: "iam.role:view" }),
  rolesCompare: route<CompareInput, RoleComparison>("GET", "/v1/iam/roles/:ref/compare", { permission: "iam.role:view" }),
  rolesCreate: route<CreateRoleInput, Role>("POST", "/v1/iam/roles", { permission: "iam.role:manage" }),
  rolesUpdate: route<UpdateRoleInput, Role>("PUT", "/v1/iam/roles/:ref", { permission: "iam.role:manage" }),
  rolesDelete: route<RoleInput, void>("DELETE", "/v1/iam/roles/:ref", { permission: "iam.role:delete" }),
  rolesReplace: route<ReplaceRoleInput, Replaced>("POST", "/v1/iam/roles/:ref/replace", { permission: "iam.role:manage" }),
  bindable: route<BindableInput, BindableRoles>("GET", "/v1/iam/bindable-roles"),
  subjectsAccess: route<SubjectInput, SubjectAccess>("GET", "/v1/iam/users/:id/access", { permission: "iam.user:view" }),
  subjectsScopes: route<SubjectInput, ScopeOptions>("GET", "/v1/iam/users/:id/scopes", { permission: "iam.user:manage" }),
  subjectsBind: route<BindInput, Binding>("POST", "/v1/iam/users/:id/bindings", { permission: "iam.user:manage" }),
  subjectsUnbind: route<UnbindInput, void>("DELETE", "/v1/iam/users/:id/bindings/:binding_id", { permission: "iam.user:manage" }),
  me: route.raw("GET", "/v1/me/access"),
} as const;
