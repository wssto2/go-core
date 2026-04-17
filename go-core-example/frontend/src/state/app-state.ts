import type { App, InjectionKey } from "vue";
import { inject, reactive, readonly } from "vue";
import type { AppState } from "../types/app-state";
import type { PageHead, PageShell } from "../types/page-shell";

type AppShellStore = {
  state: Readonly<AppState>;
  head: Readonly<PageHead>;
  replace(shell: PageShell): void;
  loadPath(path: string, lang?: string): Promise<void>;
};

const APP_STATE_KEY: InjectionKey<AppShellStore> = Symbol("app-state");

export function createAppShell(): AppShellStore {
  const state = reactive(readInitialAppState());
  const head = reactive(readDocumentHead());

  function replace(shell: PageShell): void {
    replaceReactiveObject(state, shell.bootstrap);
    replaceReactiveObject(head, shell.head);
    applyPageHead(shell.head);
    window.APP_STATE = { ...state };
  }

  async function loadPath(path: string, lang?: string): Promise<void> {
    const shell = await fetchPageShell(path, lang);
    replace(shell);
  }

  return {
    state: readonly(state),
    head: readonly(head),
    replace,
    loadPath,
  };
}

function readInitialAppState(): AppState {
  const el = document.getElementById("app-state");
  if (!el) {
    return {};
  }

  const json = el.textContent?.trim();
  if (!json) {
    return {};
  }

  try {
    const state = JSON.parse(json) as AppState;
    window.APP_STATE = state;
    return state;
  } catch (error) {
    console.error("failed to parse app state", error);
    return {};
  }
}

function readDocumentHead(): PageHead {
  return {
    title: document.title,
    metaDescription: readMetaContent("description"),
    metaKeywords: readMetaContent("keywords"),
  };
}

function readMetaContent(name: string): string {
  return (
    document
      .querySelector(`meta[name="${name}"]`)
      ?.getAttribute("content")
      ?.trim() ?? ""
  );
}

function applyPageHead(head: PageHead): void {
  document.title = head.title;
  writeMetaContent("description", head.metaDescription);
  writeMetaContent("keywords", head.metaKeywords);
}

function writeMetaContent(name: string, content: string): void {
  let meta = document.querySelector(`meta[name="${name}"]`);
  if (!meta) {
    meta = document.createElement("meta");
    meta.setAttribute("name", name);
    document.head.appendChild(meta);
  }
  meta.setAttribute("content", content);
}

function replaceReactiveObject<T extends object>(target: T, source: T): void {
  for (const key of Object.keys(target)) {
    if (!(key in source)) {
      delete (target as Record<string, unknown>)[key];
    }
  }
  Object.assign(target, source);
}

async function fetchPageShell(path: string, lang?: string): Promise<PageShell> {
  const params = new URLSearchParams({ path });
  if (lang) {
    params.set("lang", lang);
  }

  const response = await fetch(`/__page-data?${params.toString()}`, {
    headers: {
      Accept: "application/json",
    },
    credentials: "same-origin",
  });
  if (!response.ok) {
    throw new Error(`failed to load page shell for ${path}`);
  }
  return (await response.json()) as PageShell;
}

export function installAppState(app: App, shell: AppShellStore): void {
  app.provide(APP_STATE_KEY, shell);
}

export function useAppShell(): AppShellStore {
  const shell = inject(APP_STATE_KEY);
  if (!shell) {
    throw new Error("app shell is not available");
  }
  return shell;
}

export function useAppState(): Readonly<AppState> {
  return useAppShell().state;
}
