import { createApp } from "vue";
import App from "./App.vue";
import router from "./router";
import { createAppShell, installAppState } from "./state/app-state";
import "./styles/app.css";

const app = createApp(App);
const appShell = createAppShell();

router.beforeResolve(async (to) => {
  const targetLocale =
    typeof to.query.lang === "string" ? to.query.lang : undefined;

  if (to.path === appShell.state.path && (!targetLocale || targetLocale === appShell.state.locale)) {
    return;
  }

  await appShell.loadPath(to.path, targetLocale);
});

installAppState(app, appShell);
app.use(router);
app.mount("#app");
