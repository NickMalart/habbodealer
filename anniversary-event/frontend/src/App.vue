<script setup>
import {ref, onMounted} from 'vue'
import {EventsOn} from '../wailsjs/runtime'
import {
  TestMove,
  GetLogs,
  ToggleCollect,
  GetStatus,
  ResetHammer,
  SimulatePacket,
  GetIsSimulating
} from '../wailsjs/go/main/App'

const logs = ref([])
const hexInput = ref('53755042514348')
const collectEnabled = ref(false)
const status = ref('READY')
const isSimulating = ref(false)

onMounted(() => {
  GetLogs().then(result => {
    logs.value = result
  })

  const updateStatus = () => {
    GetStatus().then(s => {
      status.value = s
    })
    GetIsSimulating().then(sim => {
      isSimulating.value = sim
    })
  }
  updateStatus()
  setInterval(updateStatus, 2000)

  EventsOn('logsUpdate', newLogs => {
    logs.value = newLogs
  })
})

function handleWalk() {
  TestMove(hexInput.value)
}

function handleToggleCollect() {
  collectEnabled.value = !collectEnabled.value
  ToggleCollect(collectEnabled.value)
}

function handleResetHammer() {
  ResetHammer()
}

function handleSimulate() {
  SimulatePacket()
}

function handleClearLogs() {
  logs.value = []
}
</script>

<template>
  <div class="container">
    <div class="header">
      <h1>Anniversary Bot</h1>
      <div class="status-bar" :class="status.includes('HOLDING') ? 'status-active' : 'status-waiting'">
        {{ status }}
      </div>
    </div>

    <div class="controls">
      <div class="input-group">
        <label>Hex / Byte Packet (Manual):</label>
        <input v-model="hexInput" placeholder="e.g. 53755042514348" class="hex-input">
      </div>
      <button @click="handleWalk" class="btn-walk">Walk / Send Packet</button>
      
      <div class="divider"></div>
      
      <button @click="handleToggleCollect" :class="collectEnabled ? 'btn-stop' : 'btn-start'">
        {{ collectEnabled ? 'Disable Auto-Collector' : 'Enable Auto-Collector' }}
      </button>

      <div class="control-row">
        <button @click="handleSimulate" class="btn-sim">Simulate Hammer</button>
        <button @click="handleResetHammer" class="btn-reset">Reset Hammer</button>
      </div>

      <button @click="handleClearLogs" class="btn-clear">Clear Logs</button>

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
  background-color: #121212;
  color: #e0e0e0;
  font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
}

.header h1 {
  margin: 0 0 0.5rem 0;
  font-size: 1.5rem;
  text-align: center;
  color: #bb86fc;
}

.status-bar {
  text-align: center;
  padding: 0.4rem;
  margin-bottom: 1rem;
  border-radius: 4px;
  font-weight: bold;
  font-size: 0.9rem;
}

.status-active {
  background-color: #4caf50;
  color: white;
}

.status-waiting {
  background-color: #ff9800;
  color: white;
}

.controls {
  display: flex;
  flex-direction: column;
  gap: 1rem;
  margin-bottom: 1.5rem;
  background-color: #1e1e1e;
  padding: 1rem;
  border-radius: 8px;
  box-shadow: 0 4px 6px rgba(0,0,0,0.3);
}

.input-group {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.input-group label {
  font-size: 0.9rem;
  color: #b0b0b0;
}

.hex-input {
  background-color: #2c2c2c;
  border: 1px solid #3d3d3d;
  color: #ffffff;
  padding: 0.75rem;
  border-radius: 4px;
  font-family: monospace;
  font-size: 1rem;
}

button {
  padding: 0.75rem;
  font-size: 1rem;
  font-weight: bold;
  cursor: pointer;
  border: none;
  border-radius: 4px;
  transition: all 0.2s;
}

.btn-walk {
  background-color: #03dac6;
  color: #000000;
}

.btn-walk:hover {
  background-color: #01bfa5;
}

.btn-start {
  background-color: #4caf50;
  color: #ffffff;
}

.btn-start:hover {
  background-color: #43a047;
}

.btn-stop {
  background-color: #f44336;
  color: #ffffff;
}

.btn-stop:hover {
  background-color: #e53935;
}

.btn-reset {
  background-color: #ff9800;
  color: #ffffff;
  flex: 1;
}

.btn-sim {
  background-color: #2196f3;
  color: #ffffff;
  flex: 1;
}

.row {
  display: flex;
  gap: 0.5rem;
}

.divider {
  height: 1px;
  background-color: #333;
  margin: 0.5rem 0;
}

.btn-clear {
  background-color: #3700b3;
  color: #ffffff;
  font-size: 0.8rem;
  padding: 0.5rem;
}

.logs {
  flex-grow: 1;
  overflow-y: auto;
  background-color: #1e1e1e;
  padding: 0.75rem;
  border-radius: 8px;
  font-family: 'Consolas', monospace;
  font-size: 0.85rem;
  border: 1px solid #333;
}

.log-entry {
  margin-bottom: 0.4rem;
  border-bottom: 1px solid #2d2d2d;
  padding-bottom: 0.4rem;
  word-break: break-all;
  color: #cfd8dc;
}
</style>