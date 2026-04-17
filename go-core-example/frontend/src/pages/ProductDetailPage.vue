<template>
  <ExamplePageLayout
    :title="pageTitle"
    subtitle="This detail route is composed on the Go server, then rendered through a dedicated Vue Router page component instead of conditionals in the root app."
    :primary-action="{ to: '/products', label: 'Back to catalog' }"
    :secondary-action="{ to: '/', label: 'Overview' }"
    :total-products="totalProducts"
    :active-products="activeProducts"
    :low-stock-products="lowStockProducts"
    :locale="state.locale ?? 'en'"
    :viewer="state.viewer"
  >
    <ProductDetailCard v-if="product" :product="product" />

    <template #sidebar>
      <ExperienceSidebar :app-state="state" />
    </template>
  </ExamplePageLayout>
</template>

<script setup lang="ts">
import { computed } from "vue";
import ExperienceSidebar from "../components/ExperienceSidebar.vue";
import ProductDetailCard from "../components/ProductDetailCard.vue";
import { useCatalogSnapshot } from "../composables/useCatalogSnapshot";
import ExamplePageLayout from "../layouts/ExamplePageLayout.vue";

const { state, totalProducts, activeProducts, lowStockProducts } =
  useCatalogSnapshot();

const product = computed(() => state.product);
const pageTitle = computed(() => product.value?.name ?? "Product details");
</script>
