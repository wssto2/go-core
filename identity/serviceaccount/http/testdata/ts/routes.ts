import { route } from "@wssto2/vue-core/client";
import type { AccountPath, BindInput, BindableInput, KeyRequest, RevokeRequest, SaveInput, SubjectInput, UnbindInput, UpdateRequest } from "./schemas";
import type { ApiKeyList, BindableRoles, Binding, CreatedApiKey, ScopeOptions, ServiceAccount, ServiceAccountList, SubjectAccess } from "./entities";

export const serviceaccountRoutes = {
  index: route<void, ServiceAccountList>("GET", "/v1/iam/service-accounts", { permission: "iam.serviceaccount:view" }),
  show: route<AccountPath, ServiceAccount>("GET", "/v1/iam/service-accounts/:id", { permission: "iam.serviceaccount:view" }),
  create: route<SaveInput, ServiceAccount>("POST", "/v1/iam/service-accounts", { permission: "iam.serviceaccount:manage" }),
  update: route<UpdateRequest & { id: number }, ServiceAccount>("PUT", "/v1/iam/service-accounts/:id", { permission: "iam.serviceaccount:manage" }),
  deactivate: route<AccountPath, ServiceAccount>("POST", "/v1/iam/service-accounts/:id/deactivate", { permission: "iam.serviceaccount:manage" }),
  keys: route<AccountPath, ApiKeyList>("GET", "/v1/iam/service-accounts/:id/keys", { permission: "iam.serviceaccount:view" }),
  createkey: route<KeyRequest & { id: number }, CreatedApiKey>("POST", "/v1/iam/service-accounts/:id/keys", { permission: "iam.serviceaccount:manage" }),
  revokekey: route<RevokeRequest & { id: number }, void>("POST", "/v1/iam/service-accounts/:id/keys/:keyID/revoke", { permission: "iam.serviceaccount:revoke" }),
  bindableroles: route<BindableInput, BindableRoles>("GET", "/v1/iam/service-accounts/bindable-roles", { permission: "iam.serviceaccount:manage" }),
  access: route<SubjectInput, SubjectAccess>("GET", "/v1/iam/service-accounts/:id/access", { permission: "iam.serviceaccount:view" }),
  scopes: route<SubjectInput, ScopeOptions>("GET", "/v1/iam/service-accounts/:id/scopes", { permission: "iam.serviceaccount:manage" }),
  bind: route<BindInput, Binding>("POST", "/v1/iam/service-accounts/:id/bindings", { permission: "iam.serviceaccount:manage" }),
  unbind: route<UnbindInput, void>("DELETE", "/v1/iam/service-accounts/:id/bindings/:binding_id", { permission: "iam.serviceaccount:manage" }),
} as const;
