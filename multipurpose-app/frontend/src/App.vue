<script setup>
import { reactive, onMounted } from 'vue'
import { EventsOn } from '../wailsjs/runtime'
import { GetGEarthStatus, GetRoomUsers, GetRoomRights, AddRoomRight, RemoveRoomRight } from '../wailsjs/go/main/App'

const state = reactive({
  gearth: {
    status: 'disconnected',
    host: '',
    port: 0,
    connected: false
  },
  roomUsers: [],
  roomRights: [],
  newRightName: ''
})

async function refreshRights() {
  try {
    const rights = await GetRoomRights()
    state.roomRights = rights || []
  } catch (err) {
    console.error('Failed to get rights:', err)
  }
}

async function addRight() {
  if (!state.newRightName.trim()) return
  try {
    await AddRoomRight(state.newRightName)
    state.newRightName = ''
    await refreshRights()
  } catch (err) {
    console.error('Failed to add right:', err)
  }
}

async function removeRight(name) {
  try {
    await RemoveRoomRight(name)
    await refreshRights()
  } catch (err) {
    console.error('Failed to remove right:', err)
  }
}

onMounted(async () => {
  // Get initial status
  const initialStatus = await GetGEarthStatus()
  state.gearth.status = initialStatus.status
  if (initialStatus.host) state.gearth.host = initialStatus.host
  if (initialStatus.port) state.gearth.port = initialStatus.port

  // Get initial users
  state.roomUsers = await GetRoomUsers()
  
  // Get initial rights
  await refreshRights()

  EventsOn('gearth_status', (data) => {
    state.gearth.status = data.status
    if (data.host) state.gearth.host = data.host
    if (data.port) state.gearth.port = data.port
    if (data.connected !== undefined) state.gearth.connected = data.connected
  })

  EventsOn('room_users_updated', (users) => {
    state.roomUsers = users
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
      <div class="container">
        <div class="card rights-mgmt">
          <h2>Room Rights Management</h2>
          <div class="add-right-form">
            <input 
              v-model="state.newRightName" 
              placeholder="Enter username" 
              @keyup.enter="addRight"
            />
            <button @click="addRight">Add User</button>
          </div>
          <div v-if="state.roomRights.length === 0" class="empty-msg">
            No users have rights yet.
          </div>
          <ul v-else class="rights-list">
            <li v-for="name in state.roomRights" :key="name">
              <span class="user-name">{{ name }}</span>
              <button class="btn-remove" @click="removeRight(name)">Remove</button>
            </li>
          </ul>
        </div>

        <div class="card users-list">
          <h2>Users in Room ({{ state.roomUsers.length }})</h2>
          <div v-if="state.roomUsers.length === 0" class="empty-msg">
            No users detected yet.
          </div>
          <ul v-else>
            <li v-for="user in state.roomUsers" :key="user.chat_id">
              <span class="user-name">{{ user.name }}</span>
              <span class="user-info">
                [Chat ID: {{ user.chat_id }}]
                [Trade ID: {{ user.trade_id }}]
              </span>
            </li>
          </ul>
        </div>

        <div class="card features">
          <h2>Features</h2>
          <p>This app will contain various functions.</p>
          <!-- Future functions will be added here -->
        </div>
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
  margin-top: 40px;
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

.container {
  display: flex;
  flex-direction: column;
  gap: 2rem;
  max-width: 800px;
  margin: 0 auto;
  padding: 0 1rem;
}

.card {
  background: #2c3e50;
  padding: 1.5rem;
  border-radius: 8px;
  box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
  text-align: left;
}

.rights-mgmt .add-right-form {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 1rem;
}

.rights-mgmt input {
  flex: 1;
  padding: 0.5rem;
  border-radius: 4px;
  border: 1px solid #3e4f5f;
  background: #1b2636;
  color: white;
}

.rights-mgmt button {
  padding: 0.5rem 1rem;
  background: #3498db;
  border: none;
  border-radius: 4px;
  color: white;
  cursor: pointer;
}

.rights-mgmt button:hover {
  background: #2980b9;
}

.rights-list {
  list-style: none;
  padding: 0;
  margin: 0;
  max-height: 200px;
  overflow-y: auto;
}

.rights-list li {
  padding: 0.5rem;
  border-bottom: 1px solid #3e4f5f;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.btn-remove {
  padding: 0.2rem 0.5rem !important;
  background: #e74c3c !important;
  font-size: 0.8rem;
}

.btn-remove:hover {
  background: #c0392b !important;
}

.users-list ul {
  list-style: none;
  padding: 0;
  margin: 1rem 0 0;
  max-height: 400px;
  overflow-y: auto;
}

.users-list li {
  padding: 0.5rem;
  border-bottom: 1px solid #3e4f5f;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.users-list li:last-child {
  border-bottom: none;
}

.user-name {
  font-weight: bold;
  color: #3498db;
}

.user-info {
  font-size: 0.8rem;
  color: #95a5a6;
}

.empty-msg {
  margin-top: 1rem;
  color: #95a5a6;
  font-style: italic;
}

h1 {
  margin-bottom: 0.5rem;
}

h2 {
  color: #3498db;
  margin-top: 0;
  border-bottom: 2px solid #3498db;
  padding-bottom: 0.5rem;
}

body {
  background-color: #1b2636;
  margin: 0;
}
</style>
