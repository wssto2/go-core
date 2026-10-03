import { route } from "@wssto2/vue-core";
import type { ChangeLocaleInput, LoginAsInput, LoginInput, RefreshInput } from "./schemas";
import type { SessionResponse } from "./entities";

export const identityRoutes = {
  login: route<LoginInput, SessionResponse>("POST", "/v1/auth/login", { public: true }),
  refresh: route<RefreshInput, SessionResponse>("POST", "/v1/auth/refresh", { public: true }),
  logout: route<void, void>("POST", "/v1/auth/logout"),
  me: route<void, SessionResponse>("GET", "/v1/auth/me"),
  changeLocale: route<ChangeLocaleInput, void>("POST", "/v1/auth/change-locale"),
  loginAs: route<LoginAsInput, SessionResponse>("POST", "/v1/auth/login-as"),
} as const;
