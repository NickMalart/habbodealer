<script setup>
import { ref, onMounted, computed } from 'vue';

const stats = ref({
  totalRounds: 0,
  playerWins: 0,
  dealerWins: 0,
  playerWinRate: 0,
  dealerWinRate: 0,
  byGame: {},
  items: []
});

const loading = ref(true);
const error = ref(null);
const activeTab = ref('main');

const fetchStats = async () => {
  loading.value = true;
  error.value = null;
  try {
    if (window.go && window.go.main && window.go.main.App && window.go.main.App.GetStats) {
      const s = await window.go.main.App.GetStats();
      if (s) {
        stats.value = s;
      }
    } else {
      error.value = "Wails runtime not found. Are you running in Wails?";
    }
  } catch (e) {
    error.value = "Failed to fetch stats: " + e;
  } finally {
    loading.value = false;
  }
};

onMounted(() => {
  fetchStats();
  // Auto-refresh every 30 seconds
  setInterval(fetchStats, 30000);
});

const sortedGames = computed(() => {
  return Object.values(stats.value.byGame).sort((a, b) => b.totalRounds - a.totalRounds);
});

const sortedItems = computed(() => {
  if (!stats.value.items) return [];
  return [...stats.value.items].sort((a, b) => b.netQty - a.netQty);
});
</script>

<template>
  <div class="container">
    <div class="header card">
      <h1>🎰 Casino Statistics</h1>
      <button @click="fetchStats" :disabled="loading" class="refresh-btn">
        {{ loading ? 'Refreshing...' : 'Refresh Now' }}
      </button>
    </div>

    <div v-if="error" class="error card">
      {{ error }}
    </div>

    <!-- Tabs -->
    <div class="tabs card">
      <button @click="activeTab = 'main'" :class="{ active: activeTab === 'main' }">📈 Games</button>
      <button @click="activeTab = 'items'" :class="{ active: activeTab === 'items' }">📦 Items</button>
    </div>

    <div v-if="activeTab === 'main'">
      <!-- Overall Stats -->
      <div class="overview grid">
        <div class="card stat-card">
          <div class="stat-label">Total Rounds</div>
          <div class="stat-value">{{ stats.totalRounds }}</div>
        </div>
        <div class="card stat-card player-win">
          <div class="stat-label">Player Wins</div>
          <div class="stat-value">{{ stats.playerWins }}</div>
          <div class="stat-rate">{{ stats.playerWinRate.toFixed(1) }}%</div>
        </div>
        <div class="card stat-card dealer-win">
          <div class="stat-label">Dealer Wins</div>
          <div class="stat-value">{{ stats.dealerWins }}</div>
          <div class="stat-rate">{{ stats.dealerWinRate.toFixed(1) }}%</div>
        </div>
      </div>

      <!-- Per Game Breakdown -->
      <div class="card">
        <h2>🎮 Game Breakdown</h2>
        <table v-if="sortedGames.length > 0">
          <thead>
            <tr>
              <th>Game</th>
              <th>Rounds</th>
              <th>Player Wins</th>
              <th>Dealer Wins</th>
              <th>Player Rate</th>
              <th>Dealer Rate</th>
              <th>Casino Edge</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="game in sortedGames" :key="game.game">
              <td><strong>{{ game.game }}</strong></td>
              <td>{{ game.totalRounds }}</td>
              <td class="player-win-text">{{ game.playerWins }}</td>
              <td class="dealer-win-text">{{ game.dealerWins }}</td>
              <td class="player-win-text">{{ game.playerWinRate.toFixed(1) }}%</td>
              <td class="dealer-win-text">{{ game.dealerWinRate.toFixed(1) }}%</td>
              <td :class="game.dealerWinRate - game.playerWinRate > 0 ? 'dealer-win-text' : 'player-win-text'">
                {{ (game.dealerWinRate - game.playerWinRate).toFixed(1) }}%
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="muted">
          No game history found.
        </div>
      </div>
    </div>

    <div v-if="activeTab === 'items'">
      <div class="card">
        <h2>📦 Item Profit / Loss</h2>
        <table v-if="sortedItems.length > 0">
          <thead>
            <tr>
              <th>Item Name</th>
              <th>Won (In)</th>
              <th>Lost (Out)</th>
              <th>Net Profit</th>
              <th>Games</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in sortedItems" :key="item.name">
              <td><strong>{{ item.name }}</strong></td>
              <td class="dealer-win-text">+{{ item.wonQty }}</td>
              <td class="player-win-text">-{{ item.lostQty }}</td>
              <td :class="item.netQty >= 0 ? 'dealer-win-text' : 'player-win-text'">
                {{ item.netQty >= 0 ? '+' : '' }}{{ item.netQty }}
              </td>
              <td>{{ item.games }}</td>
            </tr>
          </tbody>
        </table>
        <div v-else class="muted">
          No item history found.
        </div>
      </div>
    </div>
  </div>
</template>

<style>
:root {
  --bg: #0f172a;
  --card-bg: #1e293b;
  --text: #f8fafc;
  --muted: #94a3b8;
  --accent: #38bdf8;
  --dealer: #4ade80;
  --player: #f87171;
  --border: rgba(255, 255, 255, 0.1);
}

body {
  margin: 0;
  background-color: var(--bg);
  color: var(--text);
  font-family: 'Inter', system-ui, -apple-system, sans-serif;
}

.container {
  max-width: 1000px;
  margin: 0 auto;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.card {
  background: var(--card-bg);
  border: 1px solid var(--border);
  border-radius: 16px;
  padding: 24px;
  box-shadow: 0 4px 6px -1px rgb(0 0 0 / 0.1);
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header h1 {
  margin: 0;
  font-size: 24px;
}

.tabs {
  display: flex;
  gap: 12px;
  padding: 12px;
  margin-bottom: -12px;
}

.tabs button {
  background: transparent;
  color: var(--muted);
  border: 1px solid var(--border);
  padding: 8px 16px;
  border-radius: 8px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
}

.tabs button.active {
  background: var(--accent);
  color: var(--bg);
  border-color: var(--accent);
}

.refresh-btn {
  background: var(--accent);
  color: var(--bg);
  border: none;
  padding: 10px 20px;
  border-radius: 8px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.2s;
}

.refresh-btn:hover {
  opacity: 0.9;
}

.refresh-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 20px;
}

.stat-card {
  text-align: center;
}

.stat-label {
  color: var(--muted);
  font-size: 14px;
  margin-bottom: 8px;
}

.stat-value {
  font-size: 32px;
  font-weight: 700;
}

.stat-rate {
  font-size: 14px;
  margin-top: 4px;
}

.player-win {
  border-bottom: 4px solid var(--player);
}

.dealer-win {
  border-bottom: 4px solid var(--dealer);
}

.player-win-text { color: var(--player); }
.dealer-win-text { color: var(--dealer); }

table {
  width: 100%;
  border-collapse: collapse;
  margin-top: 16px;
}

th {
  text-align: left;
  color: var(--muted);
  font-size: 14px;
  font-weight: 500;
  padding: 12px;
  border-bottom: 1px solid var(--border);
}

td {
  padding: 12px;
  border-bottom: 1px solid var(--border);
}

.muted {
  text-align: center;
  padding: 40px;
  color: var(--muted);
}

.error {
  background: rgba(248, 113, 113, 0.1);
  border-color: var(--dealer);
  color: var(--dealer);
}
</style>
