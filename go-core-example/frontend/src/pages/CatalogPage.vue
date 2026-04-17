<template>
  <ExamplePageLayout
    title="Server-rendered catalog route"
    subtitle="This page is routed through Vue Router on the client, but it still starts from a server-composed catalog payload and translated head metadata."
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
