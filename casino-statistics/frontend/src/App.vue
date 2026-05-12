<script setup>
import { ref, onMounted, computed } from 'vue'
import * as Events from './wailsjs/runtime/runtime'
import { GetStats, GetPlayers, GetBlockedPlayers, ToggleBlockPlayer, GetDbStatus, GetSettings, SaveSettings } from './wailsjs/go/main/App'

const activeTab = ref('dashboard')
const dbStatus = ref('Unknown')
const stats = ref({
  overall: { totalRounds: 0, playerWins: 0, dealerWins: 0, playerWinRate: 0, dealerWinRate: 0 },
  byGame: {}
})
const playerStats = ref([])
const blockedPlayers = ref([])
const searchQuery = ref('')
const isRefreshing = ref(false)
const hiddenGames = ref([])

// Session (Clocking) State
const sessionStats = ref({
  total: 0,
  playerWins: 0,
  dealerWins: 0,
  history: []
})
const selectedSessionGame = ref('PU')
const gameOptions = ['PU', 'O7', 'U7', '7', '13', '21', '6', 'H18', 'DT', 'Poker', 'Bandit']

const sessionWinRate = computed(() => {
  if (sessionStats.value.total === 0) return 0
  return (sessionStats.value.dealerWins / sessionStats.value.total) * 100
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
  let list = playerStats.value
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    list = list.filter(p => p.name.toLowerCase().includes(q))
  }
  return list.sort((a, b) => b.totalRounds - a.totalRounds)
})

async function refreshStats() {
  isRefreshing.value = true
  try {
    stats.value = await GetStats()
    playerStats.value = await GetPlayerStats()
  } finally {
    isRefreshing.value = false
  }
}

async function refreshPlayers() {
  blockedPlayers.value = await GetBlockedPlayers()
}

async function handleToggleBlock(name) {
  await ToggleBlockPlayer(name)
  await refreshPlayers()
  await refreshStats()
}

function isBlocked(name) {
  return blockedPlayers.value.includes(name)
}

onMounted(async () => {
  dbStatus.value = await GetDbStatus()
  await loadSettings()
  await refreshStats()
  await refreshPlayers()
})
</script>

<template>
  <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem;">
    <h1 style="margin: 0; font-size: 1.2rem;">Casino Statistics</h1>
    <div style="display: flex; align-items: center; gap: 1rem;">
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
    <div class="tab" :class="{ active: activeTab === 'session' }" @click="activeTab = 'session'">Live Session</div>
    <div class="tab" :class="{ active: activeTab === 'players' }" @click="activeTab = 'players'">Players</div>
    <div class="tab" :class="{ active: activeTab === 'debug' }" @click="activeTab = 'debug'">Debug</div>
  </div>

  <div class="content">
    <div v-if="activeTab === 'debug'">
      <h2>Debug Info</h2>
      <div style="background: #1b262c; padding: 1rem; border-radius: 4px; border: 1px solid #3282b8; margin-bottom: 1rem;">
        <p><strong>DB Status:</strong> {{ dbStatus }}</p>
        <p><strong>Player Stats Count:</strong> {{ playerStats.length }}</p>
        <p><strong>Blocked Count:</strong> {{ blockedPlayers.length }}</p>
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
      <div style="margin-bottom: 1rem;">
        <input v-model="searchQuery" placeholder="Search players..." style="width: 100%; padding: 0.5rem; border-radius: 4px; border: 1px solid #3282b8; background: #0f4c75; color: white;">
      </div>
      
      <table>
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
          <tr v-for="player in filteredPlayerStats" :key="player.name">
            <td :style="{ color: isBlocked(player.name) ? '#e74c3c' : 'white' }">
              {{ player.name }}
              <div v-if="isBlocked(player.name)" style="font-size: 0.7rem; color: #e74c3c;">BLOCKED</div>
            </td>
            <td>{{ player.totalRounds }}</td>
            <td class="player-win">{{ player.playerWinRate.toFixed(1) }}%</td>
            <td class="dealer-win">{{ player.dealerWinRate.toFixed(1) }}%</td>
            <td :class="{ 'dealer-win': player.dealerEdge > 0, 'player-win': player.dealerEdge < 0 }" style="font-weight: bold;">
              {{ player.dealerEdge > 0 ? '+' : '' }}{{ player.dealerEdge.toFixed(1) }}%
            </td>
            <td>
              <button @click="handleToggleBlock(player.name)" class="block-btn" :class="{ blocked: isBlocked(player.name) }" style="padding: 0.3rem 0.6rem; font-size: 0.7rem;">
                {{ isBlocked(player.name) ? 'Unblock' : 'Block' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
