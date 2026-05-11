<script setup>
import {ref, onMounted, computed} from 'vue'

const state = ref({
  connected: false,
  users: []
})
const blockedNames = ref([])
const newBlockName = ref('')
const lastWinner = ref('')
const isPicking = ref(false)

const eligibleUsers = computed(() => {
  return state.value.users.filter(user => !isBlocked(user))
})

// Helper to call Go methods
const call = async (name, ...args) => {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return await window.go.main.App[name](...args);
  }
};

const refresh = async () => {
  const s = await call('GetState')
  if (s) state.value = s
}

onMounted(async () => {
  // Load initial state
  await refresh()
  
  // Listen for updates from backend
  if (window.runtime && window.runtime.EventsOn) {
    window.runtime.EventsOn('state_updated', (updatedState) => {
      state.value = updatedState
    })
  }

  // Load blocked names from localStorage
  const stored = localStorage.getItem('blocked_names')
  if (stored) {
    blockedNames.value = JSON.parse(stored)
  }

  // Periodic fallback refresh
  setInterval(refresh, 5000)
})

const handleUpdateUsers = async () => {
  await call('UpdateUsers')
}

const addBlock = () => {
  const name = newBlockName.value.trim()
  if (name && !blockedNames.value.includes(name)) {
    blockedNames.value.push(name)
    localStorage.setItem('blocked_names', JSON.stringify(blockedNames.value))
    newBlockName.value = ''
  }
}

const removeBlock = (name) => {
  blockedNames.value = blockedNames.value.filter(n => n.toLowerCase() !== name.toLowerCase())
  localStorage.setItem('blocked_names', JSON.stringify(blockedNames.value))
}

const toggleBlock = (name) => {
  if (isBlocked(name)) {
    removeBlock(name)
  } else {
    blockedNames.value.push(name)
    localStorage.setItem('blocked_names', JSON.stringify(blockedNames.value))
  }
}

const pickWinner = async () => {
  isPicking.value = true
  lastWinner.value = ''
  
  // Update users first as requested
  await call('UpdateUsers')
  
  // Wait a bit for the packets to arrive and be processed
  setTimeout(async () => {
    const winner = await call('PickWinner', blockedNames.value)
    if (winner) {
      lastWinner.value = winner
      await call('ShoutWinner', winner)
    } else {
      alert('No eligible users found in the room!')
    }
    isPicking.value = false
  }, 1200)
}

const isBlocked = (name) => {
  return blockedNames.value.some(bn => bn.toLowerCase() === name.toLowerCase())
}
</script>

<template>
  <div class="container">
    <div class="header">
      <h1>Winner Picker</h1>
      <div class="status-badge" :class="{ connected: state.connected }">
        {{ state.connected ? 'Connected' : 'Disconnected' }}
      </div>
    </div>
    
    <div class="controls">
      <button @click="handleUpdateUsers" class="alt">Update User List</button>
      <button @click="pickWinner" :disabled="isPicking || !state.connected">
        {{ isPicking ? 'Updating & Picking...' : 'Pick Winner' }}
      </button>
    </div>

    <div v-if="lastWinner" class="winner-announce">
      Winner: {{ lastWinner }}!
    </div>

    <div class="block-section">
      <h3>Blocked Names</h3>
      <div class="block-input">
        <input v-model="newBlockName" placeholder="Enter name to block" @keyup.enter="addBlock" />
        <button @click="addBlock">Block</button>
      </div>
      <div class="blocked-list">
        <span v-for="name in blockedNames" :key="name" class="blocked-tag">
          {{ name }} <span class="remove-btn" @click="removeBlock(name)">×</span>
        </span>
      </div>
    </div>

    <div class="user-section">
      <h3>Users in Room ({{ eligibleUsers.length }})</h3>
      <div class="user-list">
        <div v-for="user in eligibleUsers" :key="user" class="user-item" @click="toggleBlock(user)" title="Click to Block">
          <span>{{ user }}</span>
        </div>
        <div v-if="eligibleUsers.length === 0" style="text-align: center; opacity: 0.5; padding: 20px;">
          No eligible users detected. Click Update or check your blocklist.
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.user-item {
  display: flex;
  justify-content: space-between;
  padding: 8px 10px;
  border-bottom: 1px solid rgba(255,255,255,0.05);
  cursor: pointer;
  transition: background 0.2s;
}

.user-item:hover {
  background: rgba(255,255,255,0.05);
}

.user-item:last-child {
  border-bottom: none;
}

.blocked {
  color: #ff6b6b;
  text-decoration: line-through;
  opacity: 0.7;
}

.container {
  display: flex;
  flex-direction: column;
  gap: 16px;
  height: 100%;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid rgba(255,255,255,0.1);
  padding-bottom: 10px;
}

h1 {
  margin: 0;
  font-size: 22px;
  color: #ffc857;
}

.status-badge {
  font-size: 11px;
  padding: 3px 8px;
  border-radius: 12px;
  background: #ff6b6b;
  color: white;
}

.status-badge.connected {
  background: #62c370;
}

.controls {
  display: flex;
  gap: 10px;
}

.controls button {
  flex: 1;
}

.block-section h3, .user-section h3 {
  margin-top: 0;
  margin-bottom: 10px;
  font-size: 16px;
  color: #b4c8ce;
}

.blocked-list {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  margin-top: 10px;
}

.blocked-tag {
    background: #355a68;
    padding: 2px 10px;
    border-radius: 12px;
    font-size: 12px;
    display: flex;
    align-items: center;
    gap: 5px;
    color: white;
    border: 1px solid rgba(255,255,255,0.1);
}

.remove-btn {
    cursor: pointer;
    font-weight: bold;
    opacity: 0.7;
}

.remove-btn:hover {
    opacity: 1;
}

.user-section {
    flex-grow: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
}

.winner-announce {
    padding: 12px;
    background: rgba(98,195,112,0.15);
    border: 1px solid #62c370;
    color: #c9f5ce;
    border-radius: 8px;
    text-align: center;
    font-size: 18px;
    font-weight: bold;
    animation: fadeIn 0.3s;
}

@keyframes fadeIn {
    from { transform: translateY(-5px); opacity: 0; }
    to { transform: translateY(0); opacity: 1; }
}
</style>
