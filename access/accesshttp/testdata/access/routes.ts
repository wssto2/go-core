import { route } from "@wssto2/vue-core";
import type { BindInput, BindableInput, CompareInput, CreateRoleInput, ReplaceRoleInput, RoleInput, SubjectInput, UnbindInput, UpdateRoleInput } from "./schemas";
import type { BindableRoles, Binding, Replaced, Role, RoleComparison, RoleHolders, RoleList, ScopeOptions, SubjectAccess } from "./entities";

export const accessRoutes = {
  rolesList: route<void, RoleList>("GET", "/iam/roles", { permission: "iam.role:view" }),
  rolesShow: route<RoleInput, Role>("GET", "/iam/roles/:ref", { permission: "iam.role:view" }),
  rolesHolders: route<RoleInput, RoleHolders>("GET", "/iam/roles/:ref/holders", { permission: "iam.role:view" }),
  rolesCompare: route<CompareInput, RoleComparison>("GET", "/iam/roles/:ref/compare", { permission: "iam.role:view" }),
  rolesCreate: route<CreateRoleInput, Role>("POST", "/iam/roles", { permission: "iam.role:manage" }),
  rolesUpdate: route<UpdateRoleInput, Role>("PUT", "/iam/roles/:ref", { permission: "iam.role:manage" }),
  rolesDelete: route<RoleInput, void>("DELETE", "/iam/roles/:ref", { permission: "iam.role:delete" }),
  rolesReplace: route<ReplaceRoleInput, Replaced>("POST", "/iam/roles/:ref/replace", { permission: "iam.role:manage" }),
  bindable: route<BindableInput, BindableRoles>("GET", "/iam/bindable-roles"),
  subjectsAccess: route<SubjectInput, SubjectAccess>("GET", "/iam/users/:id/access", { permission: "iam.user:view" }),
  subjectsScopes: route<SubjectInput, ScopeOptions>("GET", "/iam/users/:id/scopes", { permission: "iam.user:manage" }),
  subjectsBind: route<BindInput, Binding>("POST", "/iam/users/:id/bindings", { permission: "iam.user:manage" }),
  subjectsUnbind: route<UnbindInput, void>("DELETE", "/iam/users/:id/bindings/:binding_id", { permission: "iam.user:manage" }),
  me: route.raw("GET", "/me/access"),
} as const;
