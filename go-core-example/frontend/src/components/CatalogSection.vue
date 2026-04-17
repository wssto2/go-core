<template>
  <section>
    <div class="section-heading">
      <div>
        <h2>{{ catalog?.title ?? "Catalog" }}</h2>
        <p class="muted">{{ catalog?.description }}</p>
      </div>
      <span class="badge">{{ totalProducts }} items</span>
    </div>

    <p v-if="catalog?.error" class="error-banner">{{ catalog.error }}</p>

    <div v-if="products.length" class="product-grid">
      <article v-for="product in products" :key="product.id" class="product-card">
        <div class="card-top">
          <span class="sku">{{ product.sku }}</span>
          <span :class="product.active ? 'status status-active' : 'status status-draft'">
            {{ product.active ? "active" : "inactive" }}
          </span>
        </div>

        <h3>{{ product.name }}</h3>
        <p class="description">
          {{ product.description || "No description provided yet." }}
        </p>

        <div class="card-bottom">
          <strong>{{ formatPrice(product.price) }}</strong>
          <span :class="stockClass(product.stock)">
            {{ stockLabel(product.stock) }}
          </span>
        </div>

        <RouterLink
          class="details-link"
          :to="{ name: 'product-details', params: { id: product.id } }"
        >
          View details
        </RouterLink>
      </article>
    </div>

    <div v-else class="empty-state">
      <h3>No products yet</h3>
      <p>
        The catalog bootstrap is working, but there are no products to show.
      </p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { RouterLink } from "vue-router";
import type { CatalogState } from "../types/app-state";
import { formatPrice, stockClass, stockLabel } from "../utils/catalog";

const props = defineProps<{
  catalog?: CatalogState;
  totalProducts: number;
}>();

const products = computed(() => props.catalog?.products ?? []);
</script>
