<template>
  <div class="app-root">
    <header class="app-header">
      <div class="brand">
        <h1>Auto Payout Bot</h1>
        <div class="subtitle">Lightweight automated payouts</div>
      </div>
      <div class="status" :class="{ connected: isConnected }">{{ isConnected ? '● Connected' : '○ Disconnected' }}</div>
    </header>

    <nav class="primary-nav">
      <button :class="{ active: topTab === 'main' }" @click="topTab = 'main'">Main</button>
      <button :class="{ active: topTab === 'logs' }" @click="topTab = 'logs'">Logs</button>
      <div class="nav-spacer"></div>
      <button class="btn" @click="refreshQueue">Refresh</button>
    </nav>

    <main class="content">
      <!-- Main Dashboard -->
      <section v-if="topTab === 'main'" class="dashboard">
        <div class="left-col">
          <div class="card add-payout">
            <h3>Add Payout</h3>
            <div class="row">
              <input v-model="newName" placeholder="Player name" />
              <input v-model="newItem" placeholder="Item (raw name)" />
              <input v-model.number="newQty" type="number" min="1" style="width:80px" />
              <button class="btn-primary" @click="addPayout">Add</button>
            </div>
          </div>

          <div class="card payouts-card">
            <h3>Payout Queue</h3>
            <div class="table-actions">
              <button @click="clearCompleted" class="btn-small">Clear Completed</button>
              <button @click="refreshInventory" class="btn-small">Refresh Hand</button>
            </div>
            <div class="table-wrap">
              <table class="payouts-table">
                <thead>
                  <tr><th>Player</th><th>Item</th><th>Qty</th><th>Status</th><th></th></tr>
                </thead>
                <tbody>
                  <tr v-for="p in payouts" :key="p.id">
                    <td>{{ p.name }}</td>
                    <td class="mono">{{ p.itemName }}</td>
                    <td>{{ p.quantity }}</td>
                    <td>{{ p.status }}</td>
                    <td class="actions">
                      <button @click="toggleStatus(p.id)" class="btn-small">{{ p.status === 'Disabled' ? 'Enable' : 'Pause' }}</button>
                      <button @click="deletePayout(p.id)" class="btn-small danger">Delete</button>
                    </td>
                  </tr>
                  <tr v-if="payouts.length === 0"><td colspan="5" class="empty">No payouts queued</td></tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <aside class="right-col">
          <div class="card active-trade">
            <h3>Active Trade</h3>
            <div v-if="tradeInfo.active">
              <div><strong>Partner:</strong> {{ tradeInfo.partner || 'Unknown' }} ({{ tradeInfo.partnerId || '-' }})</div>
              <div style="margin-top:8px;"><strong>Elapsed:</strong> {{ formatSeconds(tradeInfo.elapsedSeconds) }}</div>
              <div><strong>Remaining:</strong> {{ formatSeconds(tradeInfo.remainingSeconds) }}</div>
              <div class="progress" style="margin-top:8px; background:#0b0c0d; height:8px; border-radius:6px; overflow:hidden;">
                <div :style="{ width: (100 - Math.max(0, (tradeInfo.remainingSeconds*100)/Math.max(1, tradeInfo.maxOpenSeconds))) + '%', background: '#646cff', height: '100%' }"></div>
              </div>
            </div>
            <div v-else class="empty">No active trade</div>
          </div>

          <div class="card banlist-card">
            <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:12px">
              <h3 style="margin:0">Ban List</h3>
              <button @click="reloadBans" class="btn-small">Reload from DB</button>
            </div>
            <div v-if="banList.length">
              <div v-for="b in banList" :key="b.key" class="ban-entry" style="display:flex;justify-content:space-between;align-items:center;padding:10px 0;border-bottom:1px solid #222">
                <div style="flex:1">
                  <div style="font-weight:600">{{ b.label }}</div>
                  <div style="font-size:0.85rem;color:var(--muted)">
                    {{ b.expiresAt === 'Lifetime' ? 'Lifetime' : formatSeconds(b.remainingSeconds) + ' left' }}
                    <span v-if="b.expiresAt !== 'Lifetime'">(expires {{ new Date(b.expiresAt).toLocaleString() }})</span>
                  </div>
                  <div v-if="b.message" style="font-size:0.85rem; color:#f88; font-style:italic; margin-top:4px">"{{ b.message }}"</div>
                </div>
                <div class="actions">
                  <button @click="editBan(b)" class="btn-small" style="margin-right:6px">Edit</button>
                  <button @click="clearBan(b.key)" class="btn-small danger">Unban</button>
                </div>
              </div>
            </div>
            <div v-else class="empty">No active bans</div>
          </div>

          <div class="card add-ban">
            <h3>Manual Ban</h3>
            <div class="row" style="display:flex; flex-direction:column; gap:8px;">
              <input v-model="banName" placeholder="Player name" />
              <select v-model="banDuration" style="background:#0b0c0e; border:1px solid #222; padding:8px; border-radius:6px; color:#fff">
                <option value="1h">1 Hour</option>
                <option value="5h">5 Hours</option>
                <option value="24h">24 Hours</option>
                <option value="1w">1 Week</option>
                <option value="1m">1 Month</option>
                <option value="lifetime">Lifetime</option>
              </select>
              <textarea v-model="banMessage" placeholder="Custom message (optional)" style="background:#0b0c0e; border:1px solid #222; padding:8px; border-radius:6px; color:#fff; min-height:60px"></textarea>
              <button class="btn-primary" @click="banPlayer" style="background:#441111">Ban Player</button>
            </div>
          </div>
          <div class="card">
            <h3>Settings</h3>
            <label>Max Unique Items</label>
            <input v-model.number="maxUniqueItems" type="number" />
            <label>Max Qty / Unique</label>
            <input v-model.number="maxQtyPerUnique" type="number" />
            <div style="margin-top:8px"><button @click="saveSettings" class="btn">Save</button></div>
          </div>
          <div class="card">
            <h3>Quick Actions</h3>
            <button @click="returnToOwner" class="btn-small">Return All To Owner</button>
          </div>
        </aside>
      </section>

      <!-- Logs / Debug View -->
      <section v-else class="logs-view">
        <div class="logs-top">
          <div class="tabs">
            <button :class="{ active: subTab === 'logs' }" @click="subTab = 'logs'">Logs</button>
            <button :class="{ active: subTab === 'debug' }" @click="subTab = 'debug'">Debug</button>
            <div class="spacer"></div>
            <input v-model="logFilter" placeholder="Filter logs" class="filter" />
            <button @click="copyAllLogs" class="btn-small">Copy</button>
            <button @click="clearAllLogs" class="btn-small">Clear</button>
          </div>
        </div>

        <div class="logs-body">
          <div v-show="subTab === 'logs'" class="logs-panel">
            <div class="logs-list" ref="logContainer">
              <div v-for="(l, i) in filteredLogs" :key="i" class="log-line" :class="{ err: l.includes('ERROR'), warn: l.includes('WARNING') }">{{ l }}</div>
              <div v-if="filteredLogs.length === 0" class="empty">No logs</div>
            </div>
          </div>

          <div v-show="subTab === 'debug'" class="debug-panel">
            <div v-for="(d, idx) in debugEvents" :key="idx" class="debug-entry">
              <div class="meta">{{ d.ts }} — {{ d.type }}</div>
              <pre class="debug-pre">{{ JSON.stringify(d.data, null, 2) }}</pre>
            </div>
            <div v-if="debugEvents.length === 0" class="empty">No debug events</div>
          </div>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup>
import { ref, onMounted, watch, nextTick, computed } from 'vue'

const topTab = ref('main')
const subTab = ref('logs')

const payouts = ref([])
const logs = ref([])
const debugEvents = ref([])

const tradeInfo = ref({ active: false, partner: '', partnerId: 0, elapsedSeconds: 0, remainingSeconds: 0, maxOpenSeconds: 0, banDurationSeconds: 0 })
const banList = ref([])

const newName = ref('')
const newItem = ref('')
const newQty = ref(1)
const isConnected = ref(false)
const logContainer = ref(null)
const logFilter = ref('')

const maxUniqueItems = ref(6)
const maxQtyPerUnique = ref(10)

const banName = ref('')
const banDuration = ref('24h')
const banMessage = ref('')

const filteredLogs = computed(() => {
  if (!logFilter.value) return logs.value
  const f = logFilter.value.toLowerCase()
  return logs.value.filter(l => l.toLowerCase().includes(f))
})

const loadInitial = async () => {
  if (window.go?.main?.App) {
    try {
      payouts.value = await window.go.main.App.GetPayouts()
      logs.value = await window.go.main.App.GetLogs()
      isConnected.value = true
      const s = await window.go.main.App.GetSettings()
      if (s) { maxUniqueItems.value = s.maxUniqueItems || 6; maxQtyPerUnique.value = s.maxQtyPerUnique || 10 }
      try {
        tradeInfo.value = await window.go.main.App.GetTradeInfo()
      } catch (e) {
        // ignore
      }
      try {
        banList.value = await window.go.main.App.GetBanList()
      } catch (e) {
        // ignore
      }
    } catch (e) {
      console.warn('initial load failed', e)
    }
  }
}

const addPayout = async () => {
  if (!newName.value || !newItem.value) return
  if (!window.go?.main?.App) return
  if (!Number.isFinite(newQty.value) || newQty.value <= 0) return
  try {
    await window.go.main.App.AddPayout(newName.value, newItem.value, newQty.value)
    newName.value = ''
    newItem.value = ''
    newQty.value = 1
    await refreshQueue()
  } catch (e) { alert('Add failed: ' + e) }
}

const deletePayout = async (id) => { if (window.go?.main?.App) await window.go.main.App.DeletePayout(id) }
const toggleStatus = async (id) => { if (window.go?.main?.App) await window.go.main.App.TogglePayoutStatus(id) }
const refreshQueue = async () => { if (window.go?.main?.App) { await window.go.main.App.RefreshQueue(); payouts.value = await window.go.main.App.GetPayouts() } }
const clearCompleted = async () => { if (window.go?.main?.App) await window.go.main.App.ClearCompleted(); await refreshQueue() }
const refreshInventory = async () => { if (window.go?.main?.App) await window.go.main.App.RefreshInventory() }
const returnToOwner = async () => { const name = prompt('Owner name:'); if (name && window.go?.main?.App) await window.go.main.App.ReturnAllToOwner(name) }

const copyAllLogs = async () => { try { const text = logs.value.join('\n'); await navigator.clipboard.writeText(text); alert('Logs copied') } catch (e) { alert('Copy failed') } }
const clearAllLogs = async () => { logs.value = []; if (window.go?.main?.App) { /* no API to clear server logs */ } }

const copyAllDebug = async () => { try { const text = debugEvents.value.map(d => `${d.ts} ${d.type}\n${JSON.stringify(d.data, null, 2)}`).join('\n\n'); await navigator.clipboard.writeText(text); alert('Copied') } catch (e) { alert('Copy failed') } }
const clearDebug = () => { debugEvents.value = [] }

const saveSettings = async () => { if (!window.go?.main?.App) return; await window.go.main.App.SaveSettings(maxUniqueItems.value, maxQtyPerUnique.value); alert('Saved') }

watch(logs, () => { nextTick(() => { if (logContainer.value) logContainer.value.scrollTop = logContainer.value.scrollHeight }) })

onMounted(() => {
  loadInitial()
  if (window.runtime) {
    window.runtime.EventsOn('payoutsUpdate', data => { payouts.value = data })
    window.runtime.EventsOn('logsUpdate', data => { logs.value = data })
    window.runtime.EventsOn('debugEvent', data => { debugEvents.value.unshift({ ts: data.ts || new Date().toLocaleString(), type: data.type || 'debug', data }); topTab.value = 'logs'; subTab.value = 'debug' })
    window.runtime.EventsOn('tradeUpdate', data => { tradeInfo.value = data })
    window.runtime.EventsOn('banListUpdate', data => { banList.value = data })
  }
})

const reloadBans = async () => {
  if (!window.go?.main?.App) return
  try {
    await window.go.main.App.ReloadBans()
  } catch (e) { alert('Reload failed: ' + e) }
}

const clearBan = async (key) => {
  if (!window.go?.main?.App) return
  try {
    await window.go.main.App.ClearBan(key)
    // rely on event emission to update banList
  } catch (e) { alert('Unban failed: ' + e) }
}

const editBan = (b) => {
  banName.value = b.label.replace('Name: ', '').replace('TradeID: ', '')
  banMessage.value = b.message
  // duration is hard to reverse exactly from remaining seconds, so we default to 24h
  banDuration.value = '24h'
}

const banPlayer = async () => {
  if (!banName.value || !window.go?.main?.App) return
  try {
    await window.go.main.App.BanPlayer(banName.value, banDuration.value, banMessage.value)
    banName.value = ''
    banMessage.value = ''
  } catch (e) { alert('Ban failed: ' + e) }
}

const formatSeconds = (s) => {
  const m = Math.floor(s / 60)
  const sec = Math.floor(s % 60)
  return `${m}:${sec.toString().padStart(2,'0')}`
}
</script>

<style scoped>
:root{--bg:#0f1113;--card:#15171a;--muted:#999;--accent:#6b7bff}
*{box-sizing:border-box}
.app-root{font-family:Inter,Segoe UI,Arial;background:var(--bg);color:#e6eef6;min-height:100vh;padding:12px}
.app-header{display:flex;align-items:center;justify-content:space-between;margin-bottom:12px}
.brand h1{margin:0;font-size:1.2rem}
.subtitle{font-size:0.85rem;color:var(--muted)}
.status{font-weight:700}
.status.connected{color:#44ff44}
.primary-nav{display:flex;gap:8px;align-items:center;padding:8px 0;margin-bottom:12px}
.primary-nav button{background:transparent;border:1px solid #2a2a2a;color:#ddd;padding:6px 10px;border-radius:6px;cursor:pointer}
.primary-nav button.active{background:var(--card);border-color:var(--accent);color:#fff}
.nav-spacer{flex:1}
.content{display:block}
.dashboard{display:flex;gap:12px}
.left-col{flex:2}
.right-col{width:320px}
.card{background:var(--card);padding:12px;border-radius:8px;margin-bottom:12px}
.add-payout .row{display:flex;gap:8px}
.add-payout input{background:#0b0c0e;border:1px solid #222;padding:8px;border-radius:6px;color:#fff}
.btn-primary{background:var(--accent);color:#fff;border:none;padding:8px 12px;border-radius:6px}
.table-wrap{max-height:50vh;overflow:auto}
.payouts-table{width:100%;border-collapse:collapse}
.payouts-table th,.payouts-table td{padding:8px;border-bottom:1px solid #222;text-align:left}
.mono{font-family:monospace;color:#9ab}
.actions button{margin-left:6px}
.logs-view{margin-top:8px}
.tabs{display:flex;gap:8px;align-items:center}
.tabs button{padding:6px 10px;border-radius:6px;border:1px solid #222;background:transparent;color:#ddd}
.tabs button.active{background:#1b1d20;border-color:var(--accent)}
.logs-top{margin-bottom:8px}
.logs-body{background:#0b0c0d;padding:8px;border-radius:6px}
.logs-list{max-height:65vh;overflow:auto;padding:8px}
.log-line{font-family:monospace;padding:4px;border-left:3px solid transparent}
.log-line.err{color:#ff8888;border-left-color:#ff4444}
.log-line.warn{color:#ffcc66;border-left-color:#ffaa00}
.debug-pre{background:#0e0f10;padding:10px;border-radius:6px;overflow:auto}
.empty{color:var(--muted);padding:12px}
.filter{background:#0b0c0d;border:1px solid #222;padding:6px;border-radius:6px;color:#ddd}
.btn-small{padding:6px 8px;border-radius:6px;background:#1a1b1d;border:1px solid #222;color:#ddd}
.btn-small.danger{background:#3a0d0d}
.danger{background:#441111;color:#fff}
label{display:block;margin-top:8px;font-size:0.85rem;color:var(--muted)}
input[type=number]{padding:8px;border-radius:6px;background:#0b0c0d;border:1px solid #222;color:#fff}
.active-trade .progress { background: #0b0c0d; border-radius: 6px; overflow: hidden }
.ban-entry { padding: 6px 0 }
</style>
 
