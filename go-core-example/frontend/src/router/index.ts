import { createRouter, createWebHistory } from "vue-router";
import CatalogPage from "../pages/CatalogPage.vue";
import OverviewPage from "../pages/OverviewPage.vue";
import ProductDetailPage from "../pages/ProductDetailPage.vue";

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/",
      name: "overview",
      component: OverviewPage,
    },
    {
      path: "/products",
      name: "catalog",
      component: CatalogPage,
    },
    {
      path: "/products/:id(\\d+)",
      name: "product-details",
      component: ProductDetailPage,
    },
  ],
  scrollBehavior() {
    return { top: 0 };
  },
});

export default router;
