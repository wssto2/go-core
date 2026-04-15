import { createApp } from "vue";
import App from "./App.vue";

type AppState = {
  appName?: string;
  env?: string;
  path?: string;
  apiBase?: string;
};

declare global {
  interface Window {
    APP_STATE?: AppState;
  }
}

function readAppState(): AppState {
  const el = document.getElementById("app-state");
  if (!el) {
    return {};
  }

  const json = el.textContent?.trim();
  if (!json) {
    return {};
  }

  try {
    return JSON.parse(json) as AppState;
  } catch (error) {
    console.error("failed to parse app state", error);
    return {};
  }
}

window.APP_STATE = readAppState();

createApp(App).mount("#app");
