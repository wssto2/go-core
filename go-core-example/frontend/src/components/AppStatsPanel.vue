<template>
  <section class="hero-panel">
    <h2>SSR / bootstrap snapshot</h2>
    <dl class="stats-list">
      <div>
        <dt>Products</dt>
        <dd>{{ totalProducts }}</dd>
      </div>
      <div>
        <dt>Active</dt>
        <dd>{{ activeProducts }}</dd>
      </div>
      <div>
        <dt>Low stock</dt>
        <dd>{{ lowStockProducts }}</dd>
      </div>
      <div>
        <dt>Locale</dt>
        <dd>{{ locale }}</dd>
      </div>
    </dl>
  </section>
</template>

<script setup lang="ts">
defineProps<{
  totalProducts: number;
  activeProducts: number;
  lowStockProducts: number;
  locale: string;
}>();
</script>
