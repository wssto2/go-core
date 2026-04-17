<template>
  <section class="card">
    <h2>What this frontend showcases</h2>
    <ul>
      <li>One shared app-state contract for the entire frontend</li>
      <li>Vue Router pages layered on top of a server-composed shell</li>
      <li>SSR metadata in the head, typed bootstrap state in the client</li>
      <li>Reusable components instead of one growing root file</li>
    </ul>
  </section>

  <section class="card">
    <h2>Bootstrap payload</h2>
    <pre>{{ prettyState }}</pre>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";
import type { AppState } from "../types/app-state";

const props = defineProps<{
  appState: AppState;
}>();

const prettyState = computed(() => JSON.stringify(props.appState, null, 2));
</script>
