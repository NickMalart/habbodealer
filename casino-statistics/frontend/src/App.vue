<script setup>
import { ref, onMounted, computed } from 'vue'
import * as Events from './wailsjs/runtime/runtime'
import { GetStats, GetPlayers, GetBlockedPlayers, ToggleBlockPlayer, GetDbStatus } from './wailsjs/go/main/App'

const activeTab = ref('dashboard')
const dbStatus = ref('Unknown')
const stats = ref({
  overall: { totalRounds: 0, playerWins: 0, dealerWins: 0, playerWinRate: 0, dealerWinRate: 0 },
  byGame: {}
})
const players = ref([])
const blockedPlayers = ref([])
const searchQuery = ref('')

const sortedGames = computed(() => {
  return Object.values(stats.value.byGame).sort((a, b) => b.totalRounds - a.totalRounds)
})

const filteredPlayers = computed(() => {
  if (!searchQuery.value) return players.value
  const q = searchQuery.value.toLowerCase()
  return players.value.filter(p => p.toLowerCase().includes(q))
})

async function refreshStats() {
  stats.value = await GetStats()
}

async function refreshPlayers() {
  players.value = await GetPlayers()
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
  await refreshStats()
  await refreshPlayers()
})
</script>

<template>
  <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem;">
    <h1 style="margin: 0; font-size: 1.2rem;">Casino Statistics</h1>
    <div :style="{ color: dbStatus === 'Connected' ? '#2ecc71' : '#e74c3c', fontSize: '0.8rem' }">
      DB: {{ dbStatus }}
    </div>
  </div>

  <div class="tabs">
    <div class="tab" :class="{ active: activeTab === 'dashboard' }" @click="activeTab = 'dashboard'">Dashboard</div>
    <div class="tab" :class="{ active: activeTab === 'players' }" @click="activeTab = 'players'">Players</div>
  </div>

  <div class="content">
    <div v-if="activeTab === 'dashboard'">
      <h2>Overall Performance</h2>
      <div class="stats-grid">
        <div class="stat-card">
          <h3>Total Rounds</h3>
          <div class="stat-value">{{ stats.overall.totalRounds }}</div>
        </div>
        <div class="stat-card">
          <h3>Player Win Rate</h3>
          <div class="stat-value player-win">{{ stats.overall.playerWinRate.toFixed(2) }}%</div>
          <div>({{ stats.overall.playerWins }} wins)</div>
        </div>
        <div class="stat-card">
          <h3>Dealer Win Rate</h3>
          <div class="stat-value dealer-win">{{ stats.overall.dealerWinRate.toFixed(2) }}%</div>
          <div>({{ stats.overall.dealerWins }} wins)</div>
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
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="activeTab === 'players'">
      <div style="margin-bottom: 1rem;">
        <input v-model="searchQuery" placeholder="Search players..." style="width: 100%; padding: 0.5rem; border-radius: 4px; border: 1px solid #3282b8; background: #0f4c75; color: white;">
      </div>
      <div class="player-list">
        <div v-for="player in filteredPlayers" :key="player" class="player-item">
          <span>{{ player }} <small v-if="isBlocked(player)" style="color: #e74c3c;">(Blocked)</small></span>
          <button @click="handleToggleBlock(player)" class="block-btn" :class="{ blocked: isBlocked(player) }">
            {{ isBlocked(player) ? 'Unblock' : 'Block' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
