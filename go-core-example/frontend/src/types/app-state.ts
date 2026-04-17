export type CatalogProduct = {
  id: number;
  name: string;
  sku: string;
  description?: string;
  price: number;
  stock: number;
  active: boolean;
};

export type CatalogState = {
  title?: string;
  description?: string;
  total?: number;
  active?: number;
  lowStock?: number;
  error?: string;
  products?: CatalogProduct[];
};

export type ProductDetailsState = {
  id?: number;
  name?: string;
  sku?: string;
  description?: string;
  price?: number;
  stock?: number;
  active?: boolean;
  imageUrl?: string;
  thumbnailUrl?: string;
  imageStatus?: string;
  error?: string;
};

export type ViewerState = {
  id: number;
  username?: string;
  policies?: string[];
};

export type AppState = {
  appName?: string;
  env?: string;
  locale?: string;
  path?: string;
  apiBase?: string;
  catalog?: CatalogState;
  product?: ProductDetailsState;
  viewer?: ViewerState;
  viewerError?: string;
};

declare global {
  interface Window {
    APP_STATE?: AppState;
  }
}
