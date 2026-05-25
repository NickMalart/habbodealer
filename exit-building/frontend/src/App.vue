<script setup>
import {ref, onMounted, computed, onUnmounted} from 'vue'
import {EventsOn} from '../wailsjs/runtime'
import {
  ExecuteExit,
  GetLogs,
  SetSchedule,
  GetScheduleStatus
} from '../wailsjs/go/main/App'

const logs = ref([])
const scheduleHours = ref(0)
const scheduleMinutes = ref(1)
const scheduleEnabled = ref(false)
const targetUnix = ref(0)
const currentTime = ref(Date.now())

let timerInterval = null

onMounted(() => {
  GetLogs().then(result => {
    logs.value = result
  })

  syncScheduleStatus()

  EventsOn('logsUpdate', newLogs => {
    logs.value = newLogs
    syncScheduleStatus()
  })

  timerInterval = setInterval(() => {
    currentTime.value = Date.now()
  }, 1000)
})

onUnmounted(() => {
  if (timerInterval) clearInterval(timerInterval)
})

function syncScheduleStatus() {
  GetScheduleStatus().then(status => {
    targetUnix.value = status.targetUnix
    scheduleEnabled.value = status.enabled
  })
}

const countdownText = computed(() => {
  if (!scheduleEnabled.value || targetUnix.value === 0) return 'Timer Inactive'
  
  const diff = (targetUnix.value * 1000) - currentTime.value
  if (diff <= 0) return 'TRIGGERING...'

  const hours = Math.floor(diff / 3600000)
  const mins = Math.floor((diff % 3600000) / 60000)
  const secs = Math.floor((diff % 60000) / 1000)

  return `${hours}h ${mins}m ${secs}s remaining`
})

function handleExecuteExit() {
  ExecuteExit()
}

function handleCopyLogs() {
  const text = logs.value.join('\n')
  navigator.clipboard.writeText(text).then(() => {
    alert('Logs copied to clipboard!')
  })
}

function toggleSchedule() {
  const newState = !scheduleEnabled.value
  scheduleEnabled.value = newState
  
  SetSchedule(parseInt(scheduleHours.value), parseInt(scheduleMinutes.value), newState).then(() => {
    syncScheduleStatus()
  })
}
</script>

<template>
  <div class="container">
    <div class="header">
      <h1>Exit-Building</h1>
    </div>

    <div class="actions">
      <!-- Relative Scheduling Section -->
      <div class="schedule-box">
        <div class="schedule-header">
          <label>Exit Room In:</label>
        </div>
        <div class="schedule-controls">
          <div class="input-group">
            <input type="number" v-model="scheduleHours" min="0" :disabled="scheduleEnabled" />
            <span>h</span>
          </div>
          <div class="input-group">
            <input type="number" v-model="scheduleMinutes" min="0" max="59" :disabled="scheduleEnabled" />
            <span>m</span>
          </div>
          <button @click="toggleSchedule" :class="scheduleEnabled ? 'btn-stop' : 'btn-step'">
            {{ scheduleEnabled ? 'Cancel' : 'Schedule' }}
          </button>
        </div>
        <!-- Dedicated Countdown Display -->
        <div v-if="scheduleEnabled" class="countdown-container">
          <span class="countdown-label">Time until trigger:</span>
          <span class="countdown-value">{{ countdownText }}</span>
        </div>
      </div>

      <hr />

      <div class="main-buttons">
        <button @click="handleExecuteExit" class="btn-full">Execute Exit Now</button>
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

.schedule-box {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  background-color: #1a1f29;
  padding: 1rem;
  border-radius: 4px;
  border: 1px solid #2d3446;
}

.schedule-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.schedule-header label {
  font-size: 0.85rem;
  color: #90a4ae;
}

.countdown-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 0.5rem;
  background-color: #0e1219;
  border-radius: 4px;
  border: 1px dashed #455a64;
}

.countdown-label {
  font-size: 0.75rem;
  color: #90a4ae;
  margin-bottom: 0.25rem;
}

.countdown-value {
  font-family: monospace;
  font-size: 1.2rem;
  color: #ffb74d;
  font-weight: bold;
}

.schedule-controls {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}

.input-group {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  background-color: #0e1219;
  padding: 0 0.5rem;
  border-radius: 4px;
  border: 1px solid #2d3446;
}

input[type="number"] {
  width: 40px;
  padding: 0.5rem 0;
  background: transparent;
  color: white;
  border: none;
  font-family: monospace;
  text-align: center;
}

/* Remove arrows from number input */
input::-webkit-outer-spin-button,
input::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}

.schedule-controls span {
  color: #90a4ae;
  font-size: 0.9rem;
}

input:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

button {
  padding: 0.5rem 1rem;
  font-size: 0.9rem;
  cursor: pointer;
  border: none;
  border-radius: 4px;
  color: white;
  transition: opacity 0.2s;
}

button:hover {
  opacity: 0.9;
}

.btn-step {
  background-color: #4caf50;
}

.btn-stop {
  background-color: #f44336;
}

.btn-full {
  background-color: #2196f3;
  flex-grow: 2;
}

.btn-copy {
  background-color: #607d8b;
  flex-grow: 1;
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