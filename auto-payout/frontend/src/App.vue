<template>
  <div class="container">
    <div class="header-section">
      <div class="title-area">
        <h1>Auto Payout Bot</h1>
        <p class="subtitle">Postgres Connected | Tracking Room Entry</p>
      </div>
      <div class="bot-status" :class="{ 'connected': isConnected }">
        {{ isConnected ? '● Bot Active' : '○ Waiting for Connection' }}
      </div>
    </div>

    <div class="payout-form">
      <div class="input-group">
        <label>Habbo Name</label>
        <input v-model="newName" placeholder="Player name" @keyup.enter="addPayout" />
      </div>
      <div class="input-group">
        <label>Raw Item Name</label>
        <input v-model="newItem" placeholder="e.g. club_sofa" @keyup.enter="addPayout" />
      </div>
      <div class="input-group">
        <label>Quantity</label>
        <input v-model.number="newQty" type="number" placeholder="Qty" style="width: 80px;" @keyup.enter="addPayout" />
      </div>
      <button @click="addPayout" class="btn-primary">Add to DB Queue</button>
    </div>

    <div class="main-content">
      <div class="payouts-table">
        <div class="table-header">
          <h3>Payout Queue</h3>
          <div class="table-actions">
            <button @click="clearCompleted" class="btn-small">Clear Finished</button>
            <button @click="refreshInventory" class="btn-small btn-highlight">Refresh My Hand</button>
            <button @click="returnToOwner" class="btn-danger btn-small">Empty Hand to Owner</button>
          </div>
        </div>
        <div class="table-container">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Item</th>
                <th>Qty</th>
                <th>Status</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in payouts" :key="p.id" :class="{ 'row-disabled': p.status === 'Disabled' }">
                <td class="player-name">{{ p.name }}</td>
                <td class="item-name">{{ p.itemName }}</td>
                <td>{{ p.quantity }}</td>
                <td>
                  <span :class="'status-' + p.status.toLowerCase().replace(' ', '-')">
                    {{ p.status }}
                  </span>
                </td>
                <td class="action-col">
                  <button @click="toggleStatus(p.id)" class="btn-icon" :title="p.status === 'Disabled' ? 'Enable' : 'Pause'">
                    {{ p.status === 'Disabled' ? '▶' : '⏸' }}
                  </button>
                  <button @click="deletePayout(p.id)" class="btn-delete" title="Remove">×</button>
                </td>
              </tr>
              <tr v-if="payouts.length === 0">
                <td colspan="5" class="empty-row">No payouts in queue. Items are saved to Postgres automatically.</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div class="logs-section">
        <h3>Activity Log</h3>
        <div class="logs-container" ref="logContainer">
          <div v-for="(log, idx) in logs" :key="idx" class="log-entry" :class="{ 'log-err': log.includes('ERROR'), 'log-warn': log.includes('WARNING') }">
            {{ log }}
          </div>
          <div v-if="logs.length === 0" class="empty-logs">Waiting for activity...</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, watch, nextTick } from 'vue'

const payouts = ref([])
const logs = ref([])
const newName = ref('')
const newItem = ref('')
const newQty = ref(1)
const isConnected = ref(false)
const logContainer = ref(null)

const loadInitialData = async () => {
  if (window.go?.main?.App) {
    payouts.value = await window.go.main.App.GetPayouts()
    logs.value = await window.go.main.App.GetLogs()
    isConnected.value = true
  }
}

const addPayout = async () => {
  if (!newName.value || !newItem.value) return
  if (!window.go?.main?.App) return
  
  try {
    await window.go.main.App.AddPayout(newName.value, newItem.value, newQty.value)
    newName.value = ''
    newItem.value = ''
    newQty.value = 1
  } catch (err) {
    alert("Database error: " + err)
  }
}

const deletePayout = async (id) => {
  if (window.go?.main?.App) {
    await window.go.main.App.DeletePayout(id)
  }
}

const toggleStatus = async (id) => {
  if (window.go?.main?.App) {
    await window.go.main.App.TogglePayoutStatus(id)
  }
}

const clearCompleted = async () => {
  if (window.go?.main?.App) {
    await window.go.main.App.ClearCompleted()
  }
}

const refreshInventory = async () => {
  if (window.go?.main?.App) {
    await window.go.main.App.RefreshInventory()
  }
}

const returnToOwner = async () => {
  const name = prompt("Enter owner name to return all hand items to:")
  if (name && window.go?.main?.App) {
    await window.go.main.App.ReturnAllToOwner(name)
  }
}

watch(logs, () => {
  nextTick(() => {
    if (logContainer.value) {
      logContainer.value.scrollTop = logContainer.value.scrollHeight
    }
  })
}, { deep: true })

onMounted(() => {
  loadInitialData()
  
  if (window.runtime) {
    window.runtime.EventsOn('payoutsUpdate', (data) => {
      payouts.value = data
    })
    window.runtime.EventsOn('logsUpdate', (data) => {
      logs.value = data
    })
  }
})
</script>

<style>
.container {
  max-width: 1200px;
  margin: 0 auto;
  padding: 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 1.5rem;
  height: 95vh;
  box-sizing: border-box;
}

.header-section {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid #333;
  padding-bottom: 1rem;
}

.subtitle { margin: 0; font-size: 0.8rem; color: #555; }

.bot-status {
  font-size: 0.9rem;
  color: #ff4444;
  background: #2a1a1a;
  padding: 6px 16px;
  border-radius: 20px;
  border: 1px solid #442222;
  font-weight: bold;
}
.bot-status.connected {
  color: #44ff44;
  background: #1a2a1a;
  border-color: #224422;
}

.payout-form {
  display: flex;
  gap: 1rem;
  background: #202020;
  padding: 1.2rem;
  border-radius: 8px;
  align-items: flex-end;
  border: 1px solid #333;
}

.input-group {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  flex: 1;
}

.input-group label {
  font-size: 0.75rem;
  color: #666;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

input {
  background: #101010;
  border: 1px solid #333;
  color: #fff;
  padding: 0.6rem;
  border-radius: 4px;
  outline: none;
  font-size: 0.9rem;
}
input:focus { border-color: #646cff; background: #000; }

.btn-primary {
  background: #646cff;
  color: white;
  border: none;
  padding: 0.6rem 1.5rem;
  border-radius: 4px;
  cursor: pointer;
  font-weight: bold;
  height: 38px;
}
.btn-primary:hover { background: #747bff; transform: translateY(-1px); }

.main-content {
  display: grid;
  grid-template-columns: 1.8fr 1fr;
  gap: 1.5rem;
  flex: 1;
  min-height: 0;
}

.payouts-table {
  background: #181818;
  border-radius: 8px;
  border: 1px solid #333;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.table-header {
  padding: 1rem;
  border-bottom: 1px solid #333;
  display: flex;
  justify-content: space-between;
  align-items: center;
  background: #202020;
}

.table-container {
  overflow-y: auto;
  flex: 1;
}

table { width: 100%; border-collapse: collapse; }
th { text-align: left; padding: 1rem; font-size: 0.75rem; color: #444; background: #1a1a1a; position: sticky; top: 0; }
td { padding: 1rem; border-bottom: 1px solid #222; font-size: 0.9rem; }

.player-name { font-weight: bold; color: #fff; }
.item-name { font-family: 'Consolas', monospace; color: #888; font-size: 0.85rem; }

.row-disabled { opacity: 0.4; }

.status-pending { color: #666; }
.status-disabled { color: #ff4444; font-style: italic; }
.status-in-room { color: #44ff44; font-weight: bold; }
.status-trading { color: #ffaa00; font-weight: bold; }
.status-completed { color: #44aaff; text-decoration: line-through; }

.btn-icon {
  background: transparent;
  border: 1px solid #333;
  color: #888;
  padding: 4px 8px;
  border-radius: 4px;
  cursor: pointer;
  margin-right: 8px;
}
.btn-icon:hover { border-color: #646cff; color: #fff; }

.btn-delete {
  background: transparent;
  border: none;
  color: #ff4444;
  font-size: 1.2rem;
  cursor: pointer;
}
.btn-delete:hover { color: #ff8888; transform: scale(1.1); }

.logs-section {
  background: #0f0f0f;
  border-radius: 8px;
  border: 1px solid #333;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.logs-container {
  padding: 1rem;
  overflow-y: auto;
  font-family: 'Consolas', monospace;
  font-size: 0.8rem;
  flex: 1;
}

.log-entry { color: #00cc00; margin-bottom: 4px; border-left: 2px solid #222; padding-left: 8px; }
.log-err { color: #ff4444; }
.log-warn { color: #ffaa00; }

.btn-highlight { background: #3a3a3a !important; color: #44ff44 !important; font-weight: bold; }
</style>
