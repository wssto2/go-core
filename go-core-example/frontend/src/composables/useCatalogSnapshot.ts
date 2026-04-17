import { computed } from "vue";
import { useAppState } from "../state/app-state";

export function useCatalogSnapshot() {
  const state = useAppState();
  const catalog = computed(() => state.catalog ?? {});
  const products = computed(() => catalog.value.products ?? []);
  const totalProducts = computed(() => catalog.value.total ?? products.value.length);
  const activeProducts = computed(() => catalog.value.active ?? 0);
  const lowStockProducts = computed(() => catalog.value.lowStock ?? 0);

  return {
    state,
    catalog,
    products,
    totalProducts,
    activeProducts,
    lowStockProducts,
  };
}
