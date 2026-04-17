<template>
  <section class="card viewer-card">
    <div>
      <h2>Signed in viewer</h2>
      <p class="muted">
        This record was resolved on the server from the bearer token before Vue
        mounted.
      </p>
    </div>
    <div class="viewer-meta">
      <strong>{{ viewer.username ?? `User #${viewer.id}` }}</strong>
      <span v-if="viewer.policies?.length">
        {{ viewer.policies.join(", ") }}
      </span>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { ViewerState } from "../types/app-state";

defineProps<{
  viewer: ViewerState;
}>();
</script>
