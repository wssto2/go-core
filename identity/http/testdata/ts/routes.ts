import { route } from "@wssto2/vue-core";
import type { ChangeLocaleInput, LoginAsInput, LoginInput, RefreshInput } from "./schemas";
import type { SessionResponse } from "./entities";

export const identityRoutes = {
  login: route<LoginInput, SessionResponse>("POST", "/auth/login", { public: true }),
  refresh: route<RefreshInput, SessionResponse>("POST", "/auth/refresh", { public: true }),
  logout: route<void, void>("POST", "/auth/logout"),
  me: route<void, SessionResponse>("GET", "/auth/me"),
  changeLocale: route<ChangeLocaleInput, void>("POST", "/auth/change-locale"),
  loginAs: route<LoginAsInput, SessionResponse>("POST", "/auth/login-as"),
} as const;
