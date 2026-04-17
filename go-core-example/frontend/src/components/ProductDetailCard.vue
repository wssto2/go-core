<template>
  <section class="card detail-card">
    <template v-if="product.error">
      <h2>Product details unavailable</h2>
      <p class="muted">
        The server recognised a product-details route, but it could not compose
        the selected product payload.
      </p>
      <p class="error-banner">{{ product.error }}</p>
    </template>

    <template v-else>
      <div class="detail-grid">
        <div class="detail-copy">
          <div class="detail-top">
            <span class="sku">{{ product.sku }}</span>
            <span :class="product.active ? 'status status-active' : 'status status-draft'">
              {{ product.active ? "active" : "inactive" }}
            </span>
          </div>

          <h2>{{ product.name }}</h2>
          <p class="description detail-description">
            {{ product.description || "No description provided yet." }}
          </p>

          <dl class="detail-specs">
            <div>
              <dt>Price</dt>
              <dd>{{ formatPrice(product.price) }}</dd>
            </div>
            <div>
              <dt>Stock</dt>
              <dd :class="stockClass(product.stock)">
                {{ stockLabel(product.stock) }}
              </dd>
            </div>
            <div>
              <dt>Image status</dt>
              <dd>{{ product.imageStatus || "not uploaded" }}</dd>
            </div>
          </dl>
        </div>

        <div class="detail-media">
          <img
            v-if="product.thumbnailUrl || product.imageUrl"
            class="product-image"
            :src="product.thumbnailUrl || product.imageUrl"
            :alt="product.name"
          />
          <div v-else class="image-placeholder">
            No product image uploaded yet.
          </div>
        </div>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import type { ProductDetailsState } from "../types/app-state";
import { formatPrice, stockClass, stockLabel } from "../utils/catalog";

defineProps<{
  product: ProductDetailsState;
}>();
</script>
