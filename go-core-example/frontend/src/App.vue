<template>
  <main class="app-shell">
    <header class="hero">
      <p class="eyebrow">go-core example</p>
      <h1>Vue 3 + Vite SPA powered by go-core</h1>
      <p class="subtitle">
        This page is served by the Go backend, while the UI is hydrated by Vue.
      </p>
    </header>

    <section class="card">
      <h2>Injected app state</h2>
      <pre>{{ prettyState }}</pre>
    </section>

    <section class="card">
      <h2>What this demonstrates</h2>
      <ul>
        <li>SPA shell rendered through the backend template</li>
        <li>Vue 3 frontend mounted from <code>frontend/src/main.ts</code></li>
        <li>Request-scoped state composed by a dedicated provider</li>
        <li>Optional viewer data resolved from injected backend services</li>
        <li>API routes remain available under <code>/api</code></li>
      </ul>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed } from 'vue'

type AppState = {
  appName?: string
  env?: string
  path?: string
  apiBase?: string
  viewer?: {
    id: number
    username?: string
    policies?: string[]
  }
  viewerError?: string
}

declare global {
  interface Window {
    APP_STATE?: AppState
  }
}

const state = window.APP_STATE ?? {}

const prettyState = computed(() => JSON.stringify(state, null, 2))
</script>

<style scoped>
:global(body) {
  margin: 0;
  font-family:
    Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont,
    "Segoe UI", sans-serif;
  background: #0f172a;
  color: #e2e8f0;
}

:global(*) {
  box-sizing: border-box;
}

.app-shell {
  max-width: 960px;
  margin: 0 auto;
  padding: 48px 20px 64px;
}

.hero {
  margin-bottom: 24px;
}

.eyebrow {
  margin: 0 0 8px;
  color: #38bdf8;
  font-size: 0.875rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

h1 {
  margin: 0 0 12px;
  font-size: clamp(2rem, 5vw, 3.5rem);
  line-height: 1.1;
}

.subtitle {
  margin: 0;
  max-width: 720px;
  color: #94a3b8;
  font-size: 1.05rem;
}

.card {
  margin-top: 20px;
  padding: 20px;
  border: 1px solid #1e293b;
  border-radius: 16px;
  background: rgba(15, 23, 42, 0.72);
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.18);
}

h2 {
  margin-top: 0;
  margin-bottom: 12px;
  font-size: 1.1rem;
}

pre {
  overflow-x: auto;
  margin: 0;
  padding: 16px;
  border-radius: 12px;
  background: #020617;
  color: #7dd3fc;
  font-size: 0.95rem;
}

ul {
  margin: 0;
  padding-left: 20px;
  color: #cbd5e1;
}

li + li {
  margin-top: 8px;
}

code {
  padding: 0.1rem 0.35rem;
  border-radius: 6px;
  background: #1e293b;
  color: #93c5fd;
}
</style>
