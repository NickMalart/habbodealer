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
            <button @click="refreshQueue" class="btn-small">Sync with DB</button>
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

      <div class="controls-panel">
        <div class="settings-panel">
          <h4>Auto-Payout Settings</h4>
          <div class="input-group small">
            <label>Max Unique Items</label>
            <input v-model.number="maxUniqueItems" type="number" />
          </div>
          <div class="input-group small">
            <label>Max Qty per Unique</label>
            <input v-model.number="maxQtyPerUnique" type="number" />
          </div>
          <div style="margin-top:8px; display:flex; gap:8px;">
            <button @click="saveSettings" class="btn-small">Save Settings</button>
            <button @click="refreshQueue" class="btn-small">Refresh Queue</button>
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- Bottom panel: Logs / Debug tabs -->
  <div class="bottom-panel">
    <div class="tabs">
      <button :class="{ active: selectedTab === 'logs' }" @click="selectedTab = 'logs'">Logs</button>
      <button :class="{ active: selectedTab === 'debug' }" @click="selectedTab = 'debug'">Debug</button>
    </div>

    <div class="tab-content">
      <div v-if="selectedTab === 'logs'">
        <div class="log-actions">
          <button @click="copyAllLogs" class="btn-small">Copy All</button>
          <button v-if="!autoScrollEnabled" @click="scrollToBottom" class="btn-small">Jump to latest</button>
        </div>
        <div class="logs-container" ref="logContainer">
          <div v-for="(log, idx) in logs" :key="idx" class="log-entry" :class="{ 'log-err': log.includes('ERROR'), 'log-warn': log.includes('WARNING') }">
            {{ log }}
          </div>
          <div v-if="logs.length === 0" class="empty-logs">Waiting for activity...</div>
        </div>
      </div>

      <div v-if="selectedTab === 'debug'">
        <div class="debug-actions" style="display:flex; gap:8px; align-items:center; margin-bottom:8px;">
          <button @click="copyAllDebug" class="btn-small">Copy All Debug</button>
          <button @click="clearDebug" class="btn-small">Clear Debug</button>
        </div>
        <div class="debug-list">
          <div v-for="(d, idx) in debugEvents" :key="idx" class="debug-entry">
            <div class="debug-header">{{ d.ts }} — {{ d.type }} <button @click="copyDebug(d)" class="btn-small" style="margin-left:8px;">Copy</button></div>
            <pre class="debug-body">{{ JSON.stringify(d.data, null, 2) }}</pre>
          </div>
          <div v-if="debugEvents.length === 0" class="empty-logs">No debug events yet.</div>
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
const maxUniqueItems = ref(6)
const maxQtyPerUnique = ref(10)

const selectedTab = ref('logs')
const debugEvents = ref([])
const autoScrollEnabled = ref(true)

const loadInitialData = async () => {
  if (window.go?.main?.App) {
    payouts.value = await window.go.main.App.GetPayouts()
    logs.value = await window.go.main.App.GetLogs()
    isConnected.value = true
    // Load persisted settings
    if (window.go.main.App.GetSettings) {
      try {
        const s = await window.go.main.App.GetSettings()
        if (s && typeof s.maxUniqueItems !== 'undefined') maxUniqueItems.value = s.maxUniqueItems
        if (s && typeof s.maxQtyPerUnique !== 'undefined') maxQtyPerUnique.value = s.maxQtyPerUnique
      } catch (e) {
        console.warn('Failed to load settings', e)
      }
    }
  }
}

const addPayout = async () => {
  if (!newName.value || !newItem.value) return
  if (!window.go?.main?.App) return

  // Basic client-side validation
  if (!Number.isFinite(newQty.value) || newQty.value <= 0) {
    alert('Quantity must be a positive number')
    return
  }

  // Call server to check how it would handle this add (authoritative)
  let serverCheck = null
  try {
    serverCheck = await window.go.main.App.CheckAddPayout(newName.value, newItem.value, newQty.value)
    debugEvents.value.unshift({ ts: new Date().toLocaleString(), type: 'server-check', data: serverCheck })
    // Keep debug tab open when checks appear
    selectedTab.value = 'debug'
  } catch (e) {
    debugEvents.value.unshift({ ts: new Date().toLocaleString(), type: 'server-error', data: String(e) })
    selectedTab.value = 'debug'
    alert('Failed to run server-side validation: ' + e)
    return
  }

  if (!serverCheck || !serverCheck.allowed) {
    const reason = serverCheck && serverCheck.reason ? serverCheck.reason : 'Rejected by server rules.'
    alert('Server validation: ' + reason)
    return
  }

  // If server intends to split into chunks, confirm before proceeding
  if (serverCheck.chunks && serverCheck.chunks.length > 1) {
    const ok = confirm(`Server will split into chunks: ${serverCheck.chunks.join(', ')}. Proceed?`)
    if (!ok) return
  }

  try {
    await window.go.main.App.AddPayout(newName.value, newItem.value, newQty.value)
    debugEvents.value.unshift({ ts: new Date().toLocaleString(), type: 'added', data: serverCheck })
    newName.value = ''
    newItem.value = ''
    newQty.value = 1
    // After adding, refresh queue and logs
    await refreshQueue()
  } catch (err) {
    alert('Database error: ' + err)
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

const refreshQueue = async () => {
  if (window.go?.main?.App) {
    await window.go.main.App.RefreshQueue()
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
    if (logContainer.value && autoScrollEnabled.value) {
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
    window.runtime.EventsOn('debugEvent', (data) => {
      // Normalize server-emitted event into {ts,type,data}
      const entry = { ts: data.ts || new Date().toLocaleString(), type: data.type || 'debug', data: data }
      debugEvents.value.unshift(entry)
      selectedTab.value = 'debug'
    })
  }
  // Attach scroll listener to detect user scrolling up
  nextTick(() => {
    if (logContainer.value) {
      logContainer.value.addEventListener('scroll', () => {
        const el = logContainer.value
        if (!el) return
        const atBottom = (el.scrollTop + el.clientHeight) >= (el.scrollHeight - 8)
        autoScrollEnabled.value = atBottom
      })
    }
  })
})

const scrollToBottom = () => {
  nextTick(() => {
    if (logContainer.value) {
      logContainer.value.scrollTop = logContainer.value.scrollHeight
      autoScrollEnabled.value = true
    }
  })
}

const copyAllLogs = async () => {
  try {
    const text = logs.value.join('\n')
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text)
      alert('Logs copied to clipboard')
    } else {
      // fallback
      const ta = document.createElement('textarea')
      ta.value = text
      document.body.appendChild(ta)
      ta.select()
      document.execCommand('copy')
      document.body.removeChild(ta)
      alert('Logs copied to clipboard')
    }
  } catch (e) {
    alert('Failed to copy logs: ' + e)
  }
}

const copyDebug = async (d) => {
  try {
    const text = JSON.stringify(d.data, null, 2)
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text)
      alert('Debug copied')
    }
  } catch (e) {
    alert('Failed to copy debug: ' + e)
  }
}

const copyAllDebug = async () => {
  try {
    const text = debugEvents.value.map(d => `${d.ts} ${d.type}\n${JSON.stringify(d.data, null, 2)}`).join('\n\n')
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text)
      alert('All debug copied')
    }
  } catch (e) {
    alert('Failed to copy debug: ' + e)
  }
}

const clearDebug = () => { debugEvents.value = [] }

const saveSettings = async () => {
  if (!window.go?.main?.App) return
  try {
    await window.go.main.App.SaveSettings(parseInt(maxUniqueItems.value), parseInt(maxQtyPerUnique.value))
    alert('Settings saved')
  } catch (e) {
    alert('Failed to save settings: ' + e)
  }
}
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

.settings-panel {
  padding: 0.8rem;
  border-bottom: 1px solid #333;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  background: #111;
}
.input-group.small { gap: 0.2rem; }
.input-group.small input { width: 120px; }

.log-entry { color: #00cc00; margin-bottom: 4px; border-left: 2px solid #222; padding-left: 8px; }
.log-err { color: #ff4444; }
.log-warn { color: #ffaa00; }

.btn-highlight { background: #3a3a3a !important; color: #44ff44 !important; font-weight: bold; }

.bottom-panel {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  height: 260px;
  background: #0b0b0b;
  border-top: 1px solid #222;
  display: flex;
  flex-direction: column;
  box-sizing: border-box;
}
.tabs { display:flex; gap:8px; padding:8px; background:#0f0f0f; border-bottom:1px solid #222; }
.tabs button { background:transparent; border:1px solid #222; color:#ccc; padding:6px 10px; border-radius:4px; cursor:pointer }
.tabs button.active { background:#222; color:#fff; }
.tab-content { padding:8px; display:flex; gap:12px; flex:1; min-height:0 }
.log-actions { display:flex; gap:8px; margin-bottom:8px }
.debug-list { overflow-y:auto; flex:1; padding:8px; font-family:Consolas, monospace }
.debug-entry { border-bottom:1px solid #222; padding:8px 0; }
.debug-header { font-weight:bold; color:#ddd }
.debug-body { background:#071; color:#eee; padding:8px; border-radius:4px; white-space:pre-wrap; font-family:Consolas, monospace; }
</style>
