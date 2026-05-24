<script setup>
import {ref, onMounted} from 'vue'
import {EventsOn} from '../wailsjs/runtime'
import {
  ExecuteCommands, 
  ExecutePickAll, 
  ExecuteRoomRefresh, 
  ExecuteHandScan, 
  ExecuteAutoDrop, 
  GetLogs
} from '../wailsjs/go/main/App'

const logs = ref([])
const selectedStep = ref('pick_all')

onMounted(() => {
  GetLogs().then(result => {
    logs.value = result
  })

  EventsOn('logsUpdate', newLogs => {
    logs.value = newLogs
  })
})

function handleExecuteFull() {
  ExecuteCommands()
}

function handleExecuteStep() {
  switch (selectedStep.value) {
    case 'pick_all': ExecutePickAll(); break;
    case 'refresh': ExecuteRoomRefresh(); break;
    case 'scan': ExecuteHandScan(); break;
    case 'drop': ExecuteAutoDrop(); break;
  }
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
      <h1>Pickup-Drop</h1>
    </div>

    <div class="actions">
      <div class="step-selector">
        <select v-model="selectedStep">
          <option value="pick_all">1. Pick All</option>
          <option value="refresh">2. Room Refresh</option>
          <option value="scan">3. Hand Scan</option>
          <option value="drop">4. Auto-Drop (Slow)</option>
        </select>
        <button @click="handleExecuteStep" class="btn-step">Execute Step</button>
      </div>
      <hr />
      <div class="main-buttons">
        <button @click="handleExecuteFull" class="btn-full">Execute Full Sequence</button>
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
}

.actions {
  margin-bottom: 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.step-selector {
  display: flex;
  gap: 0.5rem;
}

.main-buttons {
  display: flex;
  gap: 0.5rem;
}

select {
  flex-grow: 1;
  padding: 0.5rem;
  background-color: #1a1f29;
  color: white;
  border: 1px solid #2d3446;
  border-radius: 4px;
}

button {
  padding: 0.5rem 1rem;
  font-size: 0.9rem;
  cursor: pointer;
  border: none;
  border-radius: 4px;
  color: white;
}

.btn-step {
  background-color: #4caf50;
}

.btn-step:hover {
  background-color: #43a047;
}

.btn-full {
  background-color: #2196f3;
  flex-grow: 2;
}

.btn-full:hover {
  background-color: #1976d2;
}

.btn-copy {
  background-color: #607d8b;
  flex-grow: 1;
}

.btn-copy:hover {
  background-color: #455a64;
}

hr {
  border: 0;
  border-top: 1px solid #2d3446;
  width: 100%;
  margin: 0.25rem 0;
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
