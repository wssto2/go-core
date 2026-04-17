<template>
  <ExamplePageLayout
    title="SSR-ready product catalog powered by go-core"
    subtitle="A modular Vue frontend on top of a server-composed page shell, using Vue Router for page rendering and typed bootstrap state for first paint."
    :primary-action="{ to: '/products', label: 'Browse catalog' }"
    :secondary-action="{ to: '/', label: 'Overview' }"
    :total-products="totalProducts"
    :active-products="activeProducts"
    :low-stock-products="lowStockProducts"
    :locale="state.locale ?? 'en'"
    :viewer="state.viewer"
  >
    <CatalogSection :catalog="catalog" :total-products="totalProducts" />

    <template #sidebar>
      <ExperienceSidebar :app-state="state" />
    </template>
  </ExamplePageLayout>
</template>

<script setup lang="ts">
import CatalogSection from "../components/CatalogSection.vue";
import ExperienceSidebar from "../components/ExperienceSidebar.vue";
import { useCatalogSnapshot } from "../composables/useCatalogSnapshot";
import ExamplePageLayout from "../layouts/ExamplePageLayout.vue";

const { state, catalog, totalProducts, activeProducts, lowStockProducts } =
  useCatalogSnapshot();
</script>
