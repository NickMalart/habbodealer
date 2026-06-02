<script setup>
import { ref, onMounted, computed } from 'vue'
import * as Events from './wailsjs/runtime/runtime'
import { GetStats, GetPlayerStats, GetPlayerGameStats, GetPlayers, GetBlockedPlayers, ToggleBlockPlayer, GetDbStatus, GetSettings, SaveSettings, GetOwnerKey, GetLedgerStats, GetPlayerLedgerStats, PostStatsToDiscord } from './wailsjs/go/main/App'
// ... rest of imports ...

const activeTab = ref('dashboard')
// ... other refs ...
const isPostingDiscord = ref(false)

async function handlePostDiscord() {
  if (isPostingDiscord.value) return
  isPostingDiscord.value = true
  logMessage('Posting stats to Discord...')
  try {
    const res = await PostStatsToDiscord()
    logMessage(res)
    alert(res)
  } catch (err) {
    logMessage(`ERROR posting to Discord: ${err.message || err}`)
  } finally {
    isPostingDiscord.value = false
  }
}

// Modal State
const showPlayerModal = ref(false)
const selectedPlayer = ref(null)
const selectedPlayerGameStats = ref([])
const selectedPlayerLedger = ref([])
const isLoadingPlayerDetails = ref(false)

async function openPlayerDetails(player) {
  selectedPlayer.value = player
  showPlayerModal.value = true
  isLoadingPlayerDetails.value = true
  selectedPlayerLedger.value = []
  logMessage(`Opening details for player: ${player.name}`)
  
  try {
    const s = startDate.value ? startDate.value + 'T00:00:00' : ''
    const e = endDate.value ? endDate.value + 'T23:59:59' : ''
    
    // Fetch game stats and ledger stats in parallel
    const [gStats, lStats] = await Promise.all([
      GetPlayerGameStats(player.name, s, e),
      GetPlayerLedgerStats(player.name, s, e)
    ])
    
    selectedPlayerGameStats.value = gStats.sort((a, b) => b.totalRounds - a.totalRounds)
    selectedPlayerLedger.value = lStats
    logMessage(`Loaded ${gStats.length} game stats and ${lStats.length} ledger entries for ${player.name}`)
  } catch (err) {
    logMessage(`ERROR loading player details: ${err.message || err}`)
  } finally {
    isLoadingPlayerDetails.value = false
  }
}
const hiddenGames = ref([])
const startDate = ref('')
const endDate = ref('')
const appLogs = ref([])

function logMessage(msg) {
  const time = new Date().toLocaleTimeString()
  const logStr = `[${time}] ${msg}`
  appLogs.value.unshift(logStr)
  console.log(logStr)
  if (appLogs.value.length > 100) appLogs.value.pop()
}

// Session (Clocking) State
const sessionStats = ref({
  total: 0,
  playerWins: 0,
  dealerWins: 0,
  history: []
})
const selectedSessionGame = ref('PU')
const gameOptions = ['PU', 'O7', 'U7', '7', '13', '21', '6', 'H18', 'DT', 'Poker', 'Bandit', 'U10', 'O11']

const sessionWinRate = computed(() => {
  if (sessionStats.value.total === 0) return 0
  return (sessionStats.value.dealerWins / sessionStats.value.total) * 100
})

const sessionDealerEdge = computed(() => {
  if (sessionStats.value.total === 0) return 0
  const dealerRate = (sessionStats.value.dealerWins / sessionStats.value.total) * 100
  const playerRate = (sessionStats.value.playerWins / sessionStats.value.total) * 100
  return dealerRate - playerRate
})

async function loadSettings() {
  try {
    const hidden = await GetSettings('hiddenGames')
    if (hidden && hidden !== '{}') {
      hiddenGames.value = JSON.parse(hidden)
    }
  } catch (e) {
    console.error('Failed to load settings:', e)
  }
}

async function saveSettings() {
  try {
    await SaveSettings('hiddenGames', JSON.stringify(hiddenGames.value))
  } catch (e) {
    console.error('Failed to save settings:', e)
  }
}

async function toggleGameVisibility(game) {
  const index = hiddenGames.value.indexOf(game)
  if (index === -1) {
    hiddenGames.value.push(game)
  } else {
    hiddenGames.value.splice(index, 1)
  }
  await saveSettings()
}

function clockGame(winner) {
  sessionStats.value.total++
  if (winner === 'dealer') {
    sessionStats.value.dealerWins++
  } else {
    sessionStats.value.playerWins++
  }
  
  sessionStats.value.history.unshift({
    id: Date.now(),
    game: selectedSessionGame.value,
    winner: winner,
    time: new Date().toLocaleTimeString()
  })

  // Limit history to last 20
  if (sessionStats.value.history.length > 20) {
    sessionStats.value.history.pop()
  }
}

function resetSession() {
  if (confirm('Reset current session counts?')) {
    sessionStats.value = {
      total: 0,
      playerWins: 0,
      dealerWins: 0,
      history: []
    }
  }
}

const sortedGames = computed(() => {
  return Object.values(stats.value.byGame)
    .filter(g => !hiddenGames.value.includes(g.game))
    .sort((a, b) => b.totalRounds - a.totalRounds)
})

const visibleStats = computed(() => {
  const summary = {
    totalRounds: 0,
    playerWins: 0,
    dealerWins: 0,
    playerWinRate: 0,
    dealerWinRate: 0,
    dealerEdge: 0
  }
  
  Object.values(stats.value.byGame).forEach(g => {
    if (!hiddenGames.value.includes(g.game)) {
      summary.totalRounds += g.totalRounds
      summary.playerWins += g.playerWins
      summary.dealerWins += g.dealerWins
    }
  })
  
  if (summary.totalRounds > 0) {
    summary.playerWinRate = (summary.playerWins / summary.totalRounds) * 100
    summary.dealerWinRate = (summary.dealerWins / summary.totalRounds) * 100
    summary.dealerEdge = summary.dealerWinRate - summary.playerWinRate
  }
  
  return summary
})

const filteredPlayerStats = computed(() => {
  let list = Array.isArray(playerStats.value) ? [...playerStats.value] : []
  
  if (playerSubTab.value === 'blocked') {
    // Include all blocked players, even those without stats in the current range
    blockedPlayers.value.forEach(name => {
      if (!list.some(p => p.name.toLowerCase() === name.toLowerCase())) {
        list.push({
          name: name,
          totalRounds: 0,
          playerWinRate: 0,
          dealerWinRate: 0,
          dealerEdge: 0
        })
      }
    })
    list = list.filter(p => isBlocked(p.name))
  } else {
    list = list.filter(p => !isBlocked(p.name))
  }

  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    list = list.filter(p => p.name && p.name.toLowerCase().includes(q))
  }
  return list.sort((a, b) => b.totalRounds - a.totalRounds)
})

async function refreshStats() {
  if (isRefreshing.value) {
    logMessage('Refresh already in progress, skipping...')
    return
  }
  isRefreshing.value = true
  logMessage(`Refresh started (startDate="${startDate.value}", endDate="${endDate.value}")`)
  try {
    const s = startDate.value ? startDate.value + 'T00:00:00' : ''
    const e = endDate.value ? endDate.value + 'T23:59:59' : ''
    
    logMessage('Calling GetStats...')
    const res = await GetStats(s, e)
    logMessage(`GetStats returned ${Object.keys(res.byGame || {}).length} games. Total rounds: ${res.overall?.totalRounds || 0}`)
    stats.value = res
    
    logMessage('Calling GetPlayerStats...')
    const pRes = await GetPlayerStats(s, e)
    logMessage(`GetPlayerStats returned ${pRes ? pRes.length : 0} players.`)
    playerStats.value = pRes || []

    logMessage('Calling GetLedgerStats...')
    const lRes = await GetLedgerStats(s, e)
    logMessage(`GetLedgerStats returned ${lRes ? lRes.length : 0} items.`)
    ledgerStats.value = lRes || []
  } catch (err) {
    logMessage(`ERROR in refreshStats: ${err.message || err}`)
    console.error('refreshStats failed:', err)
  } finally {
    isRefreshing.value = false
  }
}

async function refreshPlayers() {
  logMessage('Refreshing blocked players...')
  blockedPlayers.value = await GetBlockedPlayers()
  logMessage(`Found ${blockedPlayers.value.length} blocked players.`)
}

async function handleToggleBlock(name) {
  logMessage(`Toggling block for: ${name}`)
  try {
    await ToggleBlockPlayer(name)
    // Small delay to ensure DB transaction is fully committed and visible
    await new Promise(r => setTimeout(r, 200))
    await refreshPlayers()
    await refreshStats()
  } catch (err) {
    logMessage(`ERROR toggling block: ${err.message || err}`)
  }
}

function isBlocked(name) {
  if (!name) return false
  const n = name.trim().toLowerCase()
  return blockedPlayers.value.some(p => p.trim().toLowerCase() === n)
}

onMounted(async () => {
  logMessage('App mounted. Initializing...')
  
  // Initial fetch
  let status = await GetDbStatus()
  ownerKey.value = await GetOwnerKey()
  await loadSettings()
  
  dbStatus.value = status
  logMessage(`DB Status: ${dbStatus.value}, Owner: ${ownerKey.value}`)

  // If still initializing, poll for a few seconds until connected or failed
  if (dbStatus.value === 'Initializing...') {
    let attempts = 0
    const maxAttempts = 10
    const interval = setInterval(async () => {
      attempts++
      status = await GetDbStatus()
      if (status !== 'Initializing...' || attempts >= maxAttempts) {
        dbStatus.value = status
        logMessage(`DB Status updated: ${dbStatus.value}`)
        clearInterval(interval)
        
        // Refresh data once we know we are connected
        if (dbStatus.value === 'Connected') {
          await refreshStats()
          await refreshPlayers()
        }
      }
    }, 1000)
  } else {
    await refreshStats()
    await refreshPlayers()
  }
})
</script>

<template>
  <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem;">
    <h1 style="margin: 0; font-size: 1.2rem;">Casino Statistics</h1>
    <div style="display: flex; align-items: center; gap: 1rem;">
      <button 
        @click="handlePostDiscord" 
        class="discord-btn" 
        :disabled="isPostingDiscord"
      >
        <span v-if="isPostingDiscord">Posting...</span>
        <span v-else>📢 Post to Discord</span>
      </button>
      <button 
        @click="refreshStats" 
        class="refresh-btn" 
        :disabled="isRefreshing"
      >
        {{ isRefreshing ? 'Refreshing...' : 'Refresh Stats' }}
      </button>
      <div :style="{ color: dbStatus === 'Connected' ? '#2ecc71' : '#e74c3c', fontSize: '0.8rem' }">
        DB: {{ dbStatus }}
      </div>
    </div>
  </div>

  <div class="tabs">
    <div class="tab" :class="{ active: activeTab === 'dashboard' }" @click="activeTab = 'dashboard'">Dashboard</div>
    <div class="tab" :class="{ active: activeTab === 'ledger' }" @click="activeTab = 'ledger'">Items Ledger</div>
    <div class="tab" :class="{ active: activeTab === 'session' }" @click="activeTab = 'session'">Live Session</div>
    <div class="tab" :class="{ active: activeTab === 'players' }" @click="activeTab = 'players'">Players</div>
    <div class="tab" :class="{ active: activeTab === 'debug' }" @click="activeTab = 'debug'">Debug</div>
  </div>

  <div v-if="activeTab === 'dashboard' || activeTab === 'players' || activeTab === 'ledger'" style="background: #0f4c75; padding: 0.8rem; border-radius: 4px; margin-bottom: 1rem; display: flex; align-items: center; gap: 1rem; border: 1px solid #3282b8;">
    <div style="display: flex; align-items: center; gap: 0.5rem;">
      <label style="font-size: 0.8rem; font-weight: bold; color: #bbe1fa;">From:</label>
      <input type="date" v-model="startDate" @change="refreshStats" style="background: #1b262c; color: white; border: 1px solid #3282b8; padding: 0.3rem; border-radius: 4px; font-size: 0.8rem;">
    </div>
    <div style="display: flex; align-items: center; gap: 0.5rem;">
      <label style="font-size: 0.8rem; font-weight: bold; color: #bbe1fa;">To:</label>
      <input type="date" v-model="endDate" @change="refreshStats" style="background: #1b262c; color: white; border: 1px solid #3282b8; padding: 0.3rem; border-radius: 4px; font-size: 0.8rem;">
    </div>
    <button @click="startDate = ''; endDate = ''; refreshStats()" style="background: #3282b8; border: none; color: white; padding: 0.3rem 0.6rem; border-radius: 4px; cursor: pointer; font-size: 0.7rem;">Clear Filter</button>
  </div>

  <div class="content">
    <div v-if="activeTab === 'ledger'">
      <h2>Items Ledger (Physical In/Out)</h2>
      <p style="font-size: 0.9rem; color: #bbe1fa; margin-bottom: 1rem;">This table shows items that physically entered or left the dealer's hand while active.</p>
      
      <table v-if="ledgerStats.length > 0">
        <thead>
          <tr>
            <th>Item Name</th>
            <th>Total In (Bets)</th>
            <th>Total Out (Payouts)</th>
            <th>Net Profit/Loss</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in ledgerStats" :key="item.name">
            <td style="font-weight: bold; color: #bbe1fa;">{{ item.name }}</td>
            <td style="color: #2ecc71;">+{{ item.totalIn }}</td>
            <td style="color: #e74c3c;">-{{ item.totalOut }}</td>
            <td :class="item.net >= 0 ? 'dealer-win' : 'player-win'" style="font-weight: bold; font-size: 1.1rem;">
              {{ item.net > 0 ? '+' : '' }}{{ item.net }}
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else style="text-align: center; padding: 3rem; color: #bbe1fa; background: #0f4c75; border-radius: 8px; border: 1px dashed #3282b8;">
        <p>No ledger data found for this period.</p>
      </div>
    </div>

    <div v-if="activeTab === 'debug'">
      <h2>Debug Info</h2>
      <div style="background: #1b262c; padding: 1rem; border-radius: 4px; border: 1px solid #3282b8; margin-bottom: 1rem;">
        <p><strong>DB Status:</strong> {{ dbStatus }}</p>
        <p><strong>Owner Key:</strong> {{ ownerKey }}</p>
        <p><strong>Player Stats Count:</strong> {{ playerStats.length }}</p>
        <p><strong>Blocked Count:</strong> {{ blockedPlayers.length }}</p>
      </div>

      <h3>App Logs</h3>
      <div style="background: #0f4c75; padding: 0.5rem; border-radius: 4px; border: 1px solid #3282b8; max-height: 400px; overflow-y: auto; font-family: monospace; font-size: 0.75rem;">
        <div v-for="(log, i) in appLogs" :key="i" style="border-bottom: 1px solid rgba(50, 130, 184, 0.3); padding: 0.2rem 0;">
          {{ log }}
        </div>
        <div v-if="appLogs.length === 0" style="color: #666; text-align: center; padding: 1rem;">No logs yet.</div>
      </div>

      <h3>Raw Player Stats (Last 5)</h3>
      <pre style="background: #0f4c75; padding: 1rem; overflow: auto; max-height: 200px; font-size: 0.8rem; color: #bbe1fa; border-radius: 4px;">{{ JSON.stringify(playerStats.slice(0, 5), null, 2) }}</pre>

      <h3>Overall Stats</h3>
      <pre style="background: #0f4c75; padding: 1rem; overflow: auto; max-height: 200px; font-size: 0.8rem; color: #bbe1fa; border-radius: 4px;">{{ JSON.stringify(stats.overall, null, 2) }}</pre>

      <h3>Hidden Games</h3>
      <pre style="background: #0f4c75; padding: 1rem; overflow: auto; max-height: 100px; font-size: 0.8rem; color: #bbe1fa; border-radius: 4px;">{{ JSON.stringify(hiddenGames, null, 2) }}</pre>
    </div>

    <div v-if="activeTab === 'session'">
      <div style="display: flex; justify-content: space-between; align-items: center;">
        <h2>Live Session (Clocking Only)</h2>
        <button @click="resetSession" style="background: #e74c3c; border: none; color: white; padding: 0.4rem 0.8rem; border-radius: 4px; cursor: pointer; font-size: 0.8rem;">Reset Session</button>
      </div>

      <div class="stats-grid" style="margin-bottom: 2rem;">
        <div class="stat-card">
          <h3>Session Total</h3>
          <div class="stat-value">{{ sessionStats.total }}</div>
        </div>
        <div class="stat-card">
          <h3>Session Wins (Dealer)</h3>
          <div class="stat-value dealer-win">{{ sessionStats.dealerWins }}</div>
        </div>
        <div class="stat-card">
          <h3>Session Win %</h3>
          <div class="stat-value dealer-win">{{ sessionWinRate.toFixed(1) }}%</div>
        </div>
        <div class="stat-card">
          <h3>Session Dealer Edge</h3>
          <div class="stat-value" :class="{ 'dealer-win': sessionDealerEdge > 0, 'player-win': sessionDealerEdge < 0 }">
            {{ sessionDealerEdge > 0 ? '+' : '' }}{{ sessionDealerEdge.toFixed(1) }}%
          </div>
        </div>
      </div>

      <div style="background: #0f4c75; padding: 1.5rem; border-radius: 8px; text-align: center; border: 2px solid #3282b8;">
        <div style="margin-bottom: 1.5rem;">
          <label style="margin-right: 1rem; font-weight: bold;">Select Game:</label>
          <select v-model="selectedSessionGame" style="padding: 0.5rem; border-radius: 4px; background: #1b262c; color: white; border: 1px solid #3282b8;">
            <option v-for="opt in gameOptions" :key="opt" :value="opt">{{ opt }}</option>
          </select>
        </div>

        <div style="display: flex; justify-content: center; gap: 2rem;">
          <button @click="clockGame('dealer')" class="clock-btn win" style="background: #2ecc71; color: white; border: none; padding: 1rem 2rem; font-size: 1.2rem; font-weight: bold; border-radius: 8px; cursor: pointer; transition: transform 0.1s;">
            DEALER WIN
          </button>
          <button @click="clockGame('player')" class="clock-btn loss" style="background: #e74c3c; color: white; border: none; padding: 1rem 2rem; font-size: 1.2rem; font-weight: bold; border-radius: 8px; cursor: pointer; transition: transform 0.1s;">
            PLAYER WIN
          </button>
        </div>
        <p style="margin-top: 1rem; font-size: 0.9rem; color: #bbe1fa;">Clocking rounds here <strong>does not</strong> save to the permanent database stats.</p>
      </div>

      <h3 style="margin-top: 2rem;">Session History</h3>
      <div v-if="sessionStats.history.length === 0" style="text-align: center; color: #666; padding: 2rem;">No games clocked this session.</div>
      <table v-else>
        <thead>
          <tr>
            <th>Time</th>
            <th>Game</th>
            <th>Winner</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="round in sessionStats.history" :key="round.id">
            <td>{{ round.time }}</td>
            <td>{{ round.game }}</td>
            <td :class="round.winner === 'dealer' ? 'dealer-win' : 'player-win'" style="font-weight: bold;">
              {{ round.winner === 'dealer' ? 'Dealer' : 'Player' }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="activeTab === 'dashboard'">
      <h2>Overall Performance</h2>
      <div class="stats-grid">
        <div class="stat-card">
          <h3>Total Rounds</h3>
          <div class="stat-value">{{ visibleStats.totalRounds }}</div>
        </div>
        <div class="stat-card">
          <h3>Dealer Win Rate</h3>
          <div class="stat-value dealer-win">{{ visibleStats.dealerWinRate.toFixed(2) }}%</div>
          <div>({{ visibleStats.dealerWins }} wins)</div>
        </div>
        <div class="stat-card">
          <h3>Dealer Edge</h3>
          <div class="stat-value" :class="{ 'dealer-win': visibleStats.dealerEdge > 0, 'player-win': visibleStats.dealerEdge < 0 }">
            {{ visibleStats.dealerEdge > 0 ? '+' : '' }}{{ visibleStats.dealerEdge.toFixed(2) }}%
          </div>
          <div style="font-size: 0.8rem; color: #bbe1fa;">(Dealer % - Player %)</div>
        </div>
      </div>

      <h2>Breakdown by Game</h2>
      <table>
        <thead>
          <tr>
            <th>Game</th>
            <th>Rounds</th>
            <th>Player Wins</th>
            <th>Dealer Wins</th>
            <th>Player %</th>
            <th>Dealer %</th>
            <th>Edge</th>
            <th style="width: 50px;"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="game in sortedGames" :key="game.game">
            <td>{{ game.game }}</td>
            <td>{{ game.totalRounds }}</td>
            <td class="player-win">{{ game.playerWins }}</td>
            <td class="dealer-win">{{ game.dealerWins }}</td>
            <td class="player-win win-rate">{{ game.playerWinRate.toFixed(1) }}%</td>
            <td class="dealer-win win-rate">{{ game.dealerWinRate.toFixed(1) }}%</td>
            <td :class="{ 'dealer-win': (game.dealerWinRate - game.playerWinRate) > 0, 'player-win': (game.dealerWinRate - game.playerWinRate) < 0 }" style="font-weight: bold;">
              {{ (game.dealerWinRate - game.playerWinRate) > 0 ? '+' : '' }}{{ (game.dealerWinRate - game.playerWinRate).toFixed(1) }}%
            </td>
            <td>
              <button @click="toggleGameVisibility(game.game)" title="Hide Game" style="background: none; border: none; color: #666; cursor: pointer; font-size: 1.2rem;">×</button>
            </td>
          </tr>
        </tbody>
      </table>

      <div v-if="hiddenGames.length > 0" style="margin-top: 2rem; padding: 1rem; border-top: 1px solid #3282b8;">
        <h3 style="font-size: 0.9rem; color: #666;">Hidden Games</h3>
        <div style="display: flex; gap: 0.5rem; flex-wrap: wrap;">
          <div v-for="game in hiddenGames" :key="game" @click="toggleGameVisibility(game)" style="background: #0f4c75; padding: 0.2rem 0.6rem; border-radius: 4px; font-size: 0.8rem; cursor: pointer; border: 1px solid #3282b8;">
            {{ game }} <span style="margin-left: 0.4rem;">+</span>
          </div>
        </div>
      </div>
    </div>

    <div v-if="activeTab === 'players'">
      <div class="sub-tabs">
        <div class="sub-tab" :class="{ active: playerSubTab === 'active' }" @click="playerSubTab = 'active'">Active Players</div>
        <div class="sub-tab" :class="{ active: playerSubTab === 'blocked' }" @click="playerSubTab = 'blocked'">Blocked Players</div>
      </div>

      <div style="margin-bottom: 1rem;">
        <input v-model="searchQuery" placeholder="Search players..." style="width: 100%; padding: 0.5rem; border-radius: 4px; border: 1px solid #3282b8; background: #0f4c75; color: white;">
      </div>
      
      <table v-if="filteredPlayerStats.length > 0">
        <thead>
          <tr>
            <th>Player Name</th>
            <th>Rounds</th>
            <th>Player %</th>
            <th>Dealer %</th>
            <th>Dealer Edge</th>
            <th style="width: 100px;">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="player in filteredPlayerStats" :key="player.name" :style="{ background: isBlocked(player.name) ? 'rgba(231, 76, 60, 0.15)' : 'transparent' }">
            <td 
              @click="openPlayerDetails(player)"
              :style="{ color: isBlocked(player.name) ? '#e74c3c' : 'white', fontWeight: isBlocked(player.name) ? 'bold' : 'normal', cursor: 'pointer' }"
              title="Click for detailed game breakdown"
            >
              <span v-if="isBlocked(player.name)" style="margin-right: 0.5rem;">🚫</span>
              <span style="text-decoration: underline; text-underline-offset: 3px;">{{ player.name }}</span>
              <div v-if="isBlocked(player.name)" style="font-size: 0.7rem; color: #e74c3c; margin-top: 0.2rem;">EXCLUDED FROM STATS</div>
            </td>
            <td>{{ player.totalRounds }}</td>
            <td class="player-win">{{ player.playerWinRate.toFixed(1) }}%</td>
            <td class="dealer-win">{{ player.dealerWinRate.toFixed(1) }}%</td>
            <td :class="{ 'dealer-win': player.dealerEdge > 0, 'player-win': player.dealerEdge < 0 }" style="font-weight: bold;">
              {{ player.dealerEdge > 0 ? '+' : '' }}{{ player.dealerEdge.toFixed(1) }}%
            </td>
            <td>
              <button 
                @click="handleToggleBlock(player.name)" 
                class="block-btn" 
                :class="{ blocked: isBlocked(player.name) }" 
                :style="{ background: isBlocked(player.name) ? '#2ecc71' : '#e74c3c', width: '80px' }"
              >
                {{ isBlocked(player.name) ? 'UNBLOCK' : 'BLOCK' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <div v-else style="text-align: center; padding: 3rem; color: #bbe1fa; background: #0f4c75; border-radius: 8px; border: 1px dashed #3282b8;">
        <div style="font-size: 2rem; margin-bottom: 1rem;">{{ playerSubTab === 'active' ? '👤' : '🚫' }}</div>
        <p style="margin: 0; font-weight: bold;">No {{ playerSubTab }} players found</p>
        <p style="margin: 0.5rem 0 0; font-size: 0.8rem; opacity: 0.7;">Try clearing filters or checking your database connection.</p>
      </div>
    </div>
  </div>

  <!-- Player Details Modal -->
  <div v-if="showPlayerModal" class="modal-overlay" @click.self="showPlayerModal = false">
    <div class="modal-container">
      <div class="modal-header">
        <h2>Player Details: {{ selectedPlayer?.name }}</h2>
        <button class="close-btn" @click="showPlayerModal = false">×</button>
      </div>
      
      <div class="modal-body">
        <div v-if="isLoadingPlayerDetails" style="text-align: center; padding: 2rem;">
          <div class="spinner"></div>
          <p>Loading breakdown...</p>
        </div>
        <div v-else>
          <div class="stats-grid" style="margin-bottom: 2rem;">
            <div class="stat-card">
              <h3>Total Rounds</h3>
              <div class="stat-value">{{ selectedPlayer?.totalRounds }}</div>
            </div>
            <div class="stat-card">
              <h3>Player Win %</h3>
              <div class="stat-value player-win">{{ selectedPlayer?.playerWinRate.toFixed(1) }}%</div>
            </div>
            <div class="stat-card">
              <h3>Dealer Win %</h3>
              <div class="stat-value dealer-win">{{ selectedPlayer?.dealerWinRate.toFixed(1) }}%</div>
            </div>
            <div class="stat-card">
              <h3>Dealer Edge</h3>
              <div class="stat-value" :class="{ 'dealer-win': selectedPlayer?.dealerEdge > 0, 'player-win': selectedPlayer?.dealerEdge < 0 }">
                {{ selectedPlayer?.dealerEdge > 0 ? '+' : '' }}{{ selectedPlayer?.dealerEdge.toFixed(1) }}%
              </div>
            </div>
          </div>

          <h3>Game Breakdown</h3>
          <table v-if="selectedPlayerGameStats.length > 0">
            <thead>
              <tr>
                <th>Game</th>
                <th>Rounds</th>
                <th>Player Wins</th>
                <th>Dealer Wins</th>
                <th>Player %</th>
                <th>Dealer %</th>
                <th>Edge</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="gs in selectedPlayerGameStats" :key="gs.game">
                <td>{{ gs.game }}</td>
                <td>{{ gs.totalRounds }}</td>
                <td class="player-win">{{ gs.playerWins }}</td>
                <td class="dealer-win">{{ gs.dealerWins }}</td>
                <td class="player-win">{{ gs.playerWinRate.toFixed(1) }}%</td>
                <td class="dealer-win">{{ gs.dealerWinRate.toFixed(1) }}%</td>
                <td :class="{ 'dealer-win': (gs.dealerWinRate - gs.playerWinRate) > 0, 'player-win': (gs.dealerWinRate - gs.playerWinRate) < 0 }" style="font-weight: bold;">
                  {{ (gs.dealerWinRate - gs.playerWinRate) > 0 ? '+' : '' }}{{ (gs.dealerWinRate - gs.playerWinRate).toFixed(1) }}%
                </td>
              </tr>
            </tbody>
          </table>
          <div v-else style="text-align: center; padding: 2rem; color: #666;">
            No game data found for this period.
          </div>

          <div style="margin-top: 2rem;">
            <h3>Item Ledger (Physical In/Out)</h3>
            <table v-if="selectedPlayerLedger.length > 0">
              <thead>
                <tr>
                  <th>Item Name</th>
                  <th>Total In (Bets)</th>
                  <th>Total Out (Payouts)</th>
                  <th>Net Profit/Loss</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in selectedPlayerLedger" :key="item.name">
                  <td style="font-weight: bold; color: #bbe1fa;">{{ item.name }}</td>
                  <td style="color: #2ecc71;">+{{ item.totalIn }}</td>
                  <td style="color: #e74c3c;">-{{ item.totalOut }}</td>
                  <td :class="item.net >= 0 ? 'dealer-win' : 'player-win'" style="font-weight: bold;">
                    {{ item.net > 0 ? '+' : '' }}{{ item.net }}
                  </td>
                </tr>
              </tbody>
            </table>
            <div v-else style="text-align: center; padding: 2rem; color: #666; background: #0f4c75; border-radius: 4px; border: 1px dashed #3282b8;">
              No item ledger data found for this player.
            </div>
          </div>
        </div>
      </div>
      
      <div class="modal-footer">
        <button @click="handleToggleBlock(selectedPlayer.name); showPlayerModal = false" class="block-btn" style="width: 100%;">
          {{ isBlocked(selectedPlayer.name) ? 'UNBLOCK PLAYER' : 'BLOCK PLAYER' }}
        </button>
      </div>
    </div>
  </div>
</template>
