import type { AppState } from "./app-state";

export type PageHead = {
  title: string;
  metaDescription: string;
  metaKeywords: string;
};

export type PageShell = {
  head: PageHead;
  bootstrap: AppState;
};
