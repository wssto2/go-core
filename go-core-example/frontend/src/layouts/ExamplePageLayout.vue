<template>
  <main class="catalog-page">
    <header class="hero">
      <div class="hero-copy">
        <p class="eyebrow">go-core example</p>
        <h1>{{ title }}</h1>
        <p class="subtitle">{{ subtitle }}</p>
        <div class="hero-actions">
          <RouterLink class="button button-primary" :to="primaryAction.to">
            {{ primaryAction.label }}
          </RouterLink>
          <RouterLink class="button button-secondary" :to="secondaryAction.to">
            {{ secondaryAction.label }}
          </RouterLink>
        </div>
      </div>

      <AppStatsPanel
        :total-products="totalProducts"
        :active-products="activeProducts"
        :low-stock-products="lowStockProducts"
        :locale="locale"
      />
    </header>

    <ViewerCard v-if="viewer" :viewer="viewer" />

    <section class="catalog-layout">
      <section class="catalog-column">
        <slot />
      </section>

      <aside class="sidebar">
        <slot name="sidebar" />
      </aside>
    </section>
  </main>
</template>

<script setup lang="ts">
import { RouterLink } from "vue-router";
import type { ViewerState } from "../types/app-state";
import AppStatsPanel from "../components/AppStatsPanel.vue";
import ViewerCard from "../components/ViewerCard.vue";

type ActionLink = {
  to: string;
  label: string;
};

defineProps<{
  title: string;
  subtitle: string;
  primaryAction: ActionLink;
  secondaryAction: ActionLink;
  totalProducts: number;
  activeProducts: number;
  lowStockProducts: number;
  locale: string;
  viewer?: ViewerState;
}>();
</script>
