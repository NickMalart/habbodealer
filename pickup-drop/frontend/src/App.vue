<script setup>
import {ref, onMounted} from 'vue'
import {EventsOn} from '../wailsjs/runtime'
import {ExecuteCommands, GetLogs} from '../wailsjs/go/main/App'

const logs = ref([])
const running = ref(false)

onMounted(() => {
  GetLogs().then(result => {
    logs.value = result
  })

  EventsOn('logsUpdate', newLogs => {
    logs.value = newLogs
  })
})

function handleExecute() {
  ExecuteCommands()
}
</script>

<template>
  <div class="container">
    <div class="header">
      <h1>Pickup-Drop</h1>
    </div>

    <div class="actions">
      <button @click="handleExecute">Execute Commands</button>
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
}

.actions {
  margin-bottom: 1rem;
}

button {
  padding: 0.5rem 1rem;
  font-size: 1rem;
  cursor: pointer;
  background-color: #2196f3;
  color: white;
  border: none;
  border-radius: 4px;
}

button:hover {
  background-color: #1976d2;
}

.logs {
  flex-grow: 1;
  overflow-y: auto;
  background-color: #1a1f29;
  padding: 0.5rem;
  border-radius: 4px;
  font-family: monospace;
  font-size: 0.85rem;
}

.log-entry {
  margin-bottom: 0.25rem;
  border-bottom: 1px solid #2d3446;
  padding-bottom: 0.25rem;
}
</style>
