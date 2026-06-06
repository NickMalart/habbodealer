<script setup>
import {ref, onMounted} from 'vue'
import {EventsOn} from '../wailsjs/runtime'
import {
  ToggleEvent,
  ResetHammer,
  GetLogs,
  GetStatus
} from '../wailsjs/go/main/App'

const logs = ref([])
const enabled = ref(false)
const status = ref('IDLE')

onMounted(() => {
  GetLogs().then(result => {
    logs.value = result
  })

  GetStatus().then(result => {
    status.value = result
  })

  EventsOn('logsUpdate', newLogs => {
    logs.value = newLogs
  })

  EventsOn('statusUpdate', newStatus => {
    status.value = newStatus
  })
})

function handleToggle() {
  enabled.value = !enabled.value
  ToggleEvent(enabled.value)
}

function handleReset() {
  ResetHammer()
}

function handleCopyLogs() {
  const text = logs.value.join('\n')
  navigator.clipboard.writeText(text).then(() => {
    alert('Logs copied to clipboard!')
  })
}
</script>

<template>
  <div class="container">
    <div class="header">
      <h1>Anniversary Bot</h1>
    </div>

    <div class="actions">
      <div class="status-box">
        <div :class="['status-indicator', enabled ? 'active' : 'inactive']">
          {{ status }}
        </div>
        <button @click="handleToggle" :class="enabled ? 'btn-stop' : 'btn-start'">
          {{ enabled ? 'Disable' : 'Enable' }}
        </button>
      </div>

      <div class="controls">
        <button @click="handleReset" class="btn-reset">Reset Hammer State</button>
        <button @click="handleCopyLogs" class="btn-copy">Copy Logs</button>
      </div>
    </div>

    <div class="logs">
      <div v-for="(log, index) in logs.slice().reverse()" :key="index" class="log-entry">
        {{ log }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.container {
  display: flex;
  flex-direction: column;
  height: 100vh;
  padding: 1rem;
  box-sizing: border-box;
  background-color: #0e1219;
  color: #e0e0e0;
  font-family: sans-serif;
}

.header h1 {
  margin: 0 0 1rem 0;
  font-size: 1.5rem;
  text-align: center;
}

.actions {
  margin-bottom: 1rem;
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.status-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.75rem;
  background-color: #1a1f29;
  padding: 1.5rem;
  border-radius: 8px;
  border: 1px solid #2d3446;
}

.status-indicator {
  font-weight: bold;
  font-size: 1.2rem;
  padding: 0.5rem 2rem;
  border-radius: 20px;
}

.active {
  background-color: #2e7d32;
  color: #a5d6a7;
  box-shadow: 0 0 10px rgba(46, 125, 50, 0.5);
}

.inactive {
  background-color: #c62828;
  color: #ef9a9a;
}

.controls {
  display: flex;
  gap: 0.5rem;
}

button {
  padding: 0.75rem 1rem;
  font-size: 1rem;
  cursor: pointer;
  border: none;
  border-radius: 4px;
  color: white;
  transition: all 0.2s;
  flex-grow: 1;
}

button:hover {
  opacity: 0.9;
  transform: translateY(-1px);
}

.btn-start {
  background-color: #4caf50;
  width: 100%;
}

.btn-stop {
  background-color: #f44336;
  width: 100%;
}

.btn-reset {
  background-color: #ff9800;
}

.btn-copy {
  background-color: #607d8b;
}

.logs {
  flex-grow: 1;
  overflow-y: auto;
  background-color: #1a1f29;
  padding: 0.5rem;
  border-radius: 4px;
  font-family: monospace;
  font-size: 0.85rem;
  border: 1px solid #2d3446;
}

.log-entry {
  margin-bottom: 0.25rem;
  border-bottom: 1px solid #232a37;
  padding-bottom: 0.25rem;
  word-break: break-all;
}
</style>