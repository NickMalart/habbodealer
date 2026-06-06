<script setup>
import {ref, onMounted} from 'vue'
import {EventsOn} from '../wailsjs/runtime'
import {GetLogs} from '../wailsjs/go/main/App'

const logs = ref([])

onMounted(() => {
  GetLogs().then(result => {
    logs.value = result
  })

  EventsOn('logsUpdate', newLogs => {
    logs.value = newLogs
  })
})
</script>

<template>
  <div class="container">
    <div class="header">
      <h1>Fresh Bot</h1>
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
  font-family: sans-serif;
}

.header h1 {
  margin: 0 0 1rem 0;
  font-size: 1.5rem;
  text-align: center;
  color: #bb86fc;
}

.logs {
  flex-grow: 1;
  overflow-y: auto;
  background-color: #1e1e1e;
  padding: 0.75rem;
  border-radius: 8px;
  font-family: monospace;
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
