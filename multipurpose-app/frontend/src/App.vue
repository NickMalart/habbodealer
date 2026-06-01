<script setup>
import { reactive, onMounted } from 'vue'
import { EventsOn } from '../wailsjs/runtime'

const state = reactive({
  gearth: {
    status: 'disconnected',
    host: '',
    port: 0,
    connected: false
  }
})

onMounted(() => {
  EventsOn('gearth_status', (data) => {
    state.gearth.status = data.status
    if (data.host) state.gearth.host = data.host
    if (data.port) state.gearth.port = data.port
    if (data.connected !== undefined) state.gearth.connected = data.connected
  })
})
</script>

<template>
  <div id="app">
    <header>
      <h1>Multipurpose App</h1>
      <div :class="['status-badge', state.gearth.status]">
        G-Earth: {{ state.gearth.status }}
        <span v-if="state.gearth.status === 'connected'">
          ({{ state.gearth.host }}:{{ state.gearth.port }})
        </span>
      </div>
    </header>
    <main>
      <div class="card">
        <h2>Features</h2>
        <p>This app will contain various functions.</p>
        <!-- Future functions will be added here -->
      </div>
    </main>
  </div>
</template>

<style>
#app {
  font-family: Avenir, Helvetica, Arial, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  text-align: center;
  color: #ffffff;
  margin-top: 60px;
}

header {
  margin-bottom: 2rem;
}

.status-badge {
  display: inline-block;
  padding: 0.5rem 1rem;
  border-radius: 20px;
  font-weight: bold;
  text-transform: capitalize;
}

.status-badge.disconnected {
  background-color: #e74c3c;
}

.status-badge.initialized {
  background-color: #f39c12;
}

.status-badge.connected {
  background-color: #2ecc71;
}

.card {
  background: #2c3e50;
  padding: 2rem;
  border-radius: 8px;
  max-width: 600px;
  margin: 0 auto;
  box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
}

h1 {
  margin-bottom: 0.5rem;
}

h2 {
  color: #3498db;
}

body {
  background-color: #1b2636;
  margin: 0;
}
</style>
