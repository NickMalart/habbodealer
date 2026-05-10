<script setup>
import {ref, onMounted} from 'vue'

const users = ref([])
const blockedNames = ref([])
const newBlockName = ref('')
const lastWinner = ref('')
const isPicking = ref(false)

// Helper to call Go methods
const call = async (name, ...args) => {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return await window.go.main.App[name](...args);
  }
};

onMounted(async () => {
  // Load initial users
  const u = await call('GetUsers')
  if (u) users.value = u
  
  // Listen for updates from backend
  if (window.runtime && window.runtime.EventsOn) {
    window.runtime.EventsOn('users_updated', (updatedUsers) => {
      users.value = updatedUsers
    })
  }

  // Load blocked names from localStorage
  const stored = localStorage.getItem('blocked_names')
  if (stored) {
    blockedNames.value = JSON.parse(stored)
  }
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
  blockedNames.value = blockedNames.value.filter(n => n !== name)
  localStorage.setItem('blocked_names', JSON.stringify(blockedNames.value))
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
  }, 1000)
}

const isBlocked = (name) => {
  return blockedNames.value.some(bn => bn.toLowerCase() === name.toLowerCase())
}
</script>

<template>
  <div class="container">
    <h1>Winner Picker</h1>
    
    <div class="controls">
      <button @click="handleUpdateUsers">Update User List</button>
      <button @click="pickWinner" :disabled="isPicking">
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
      <h3>Users in Room ({{ users.length }})</h3>
      <div class="user-list">
        <div v-for="user in users" :key="user" class="user-item">
          <span :class="{ blocked: isBlocked(user) }">{{ user }}</span>
          <span v-if="isBlocked(user)" style="font-size: 10px; color: #e74c3c;">(BLOCKED)</span>
        </div>
        <div v-if="users.length === 0" style="text-align: center; opacity: 0.5; padding: 20px;">
          No users detected. Click Update or wait for room movement.
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
h1 {
  margin-top: 0;
  text-align: center;
  color: #3498db;
}

.controls {
  display: flex;
  gap: 10px;
}

.controls button {
  flex: 1;
}

.block-section h3, .user-section h3 {
  margin-bottom: 10px;
  border-bottom: 1px solid #333;
  padding-bottom: 5px;
}

.blocked-list {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  margin-top: 10px;
}

.blocked-tag {
    background-color: #c0392b;
    padding: 2px 8px;
    border-radius: 12px;
    font-size: 12px;
    display: flex;
    align-items: center;
    gap: 5px;
    color: white;
}

.remove-btn {
    cursor: pointer;
    font-weight: bold;
}

.user-section {
    flex-grow: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
}
</style>
