<script setup>
import { ref, onMounted, computed } from 'vue';

const stats = ref({
  totalRounds: 0,
  playerWins: 0,
  dealerWins: 0,
  playerWinRate: 0,
  dealerWinRate: 0,
  byGame: {},
  items: [],
  players: []
});

const blockedPlayers = ref([]);
const newBlockedPlayer = ref('');
const loading = ref(true);
const error = ref(null);
const activeTab = ref('main');

const selectedPlayer = ref(null);
const showModal = ref(false);
const loadingPlayer = ref(false);

const fetchStats = async () => {
  loading.value = true;
  error.value = null;
  try {
    if (window.go && window.go.main && window.go.main.App && window.go.main.App.GetStats) {
      const s = await window.go.main.App.GetStats();
      if (s) {
        stats.value = s;
      }
      const b = await window.go.main.App.GetBlockedPlayers();
      if (b) {
        blockedPlayers.value = b;
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

const openPlayerDetails = async (playerName) => {
  loadingPlayer.value = true;
  showModal.value = true;
  selectedPlayer.value = null;
  try {
    if (window.go && window.go.main && window.go.main.App && window.go.main.App.GetPlayerDetails) {
      const details = await window.go.main.App.GetPlayerDetails(playerName);
      selectedPlayer.value = details;
    }
  } catch (e) {
    console.error("Failed to fetch player details:", e);
  } finally {
    loadingPlayer.value = false;
  }
};

const closeModal = () => {
  showModal.value = false;
  selectedPlayer.value = null;
};

const blockPlayer = async (name) => {
  if (!name) name = newBlockedPlayer.value;
  if (!name) return;
  try {
    await window.go.main.App.BlockPlayer(name);
    newBlockedPlayer.value = '';
    fetchStats();
  } catch (e) {
    alert("Failed to block player: " + e);
  }
};

const unblockPlayer = async (name) => {
  try {
    await window.go.main.App.UnblockPlayer(name);
    fetchStats();
  } catch (e) {
    alert("Failed to unblock player: " + e);
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

const sortedPlayers = computed(() => {
  if (!stats.value.players) return [];
  return [...stats.value.players].sort((a, b) => b.totalRounds - a.totalRounds);
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
      <button @click="activeTab = 'players'" :class="{ active: activeTab === 'players' }">👥 Players</button>
      <button @click="activeTab = 'blocklist'" :class="{ active: activeTab === 'blocklist' }">🚫 Blocklist</button>
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

    <div v-if="activeTab === 'players'">
      <div class="card">
        <h2>👥 Player Performance</h2>
        <table v-if="sortedPlayers.length > 0">
          <thead>
            <tr>
              <th>Player Name</th>
              <th>Games Played</th>
              <th>Wins</th>
              <th>Losses</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="player in sortedPlayers" :key="player.playerName" @click="openPlayerDetails(player.playerName)" class="clickable-row">
              <td><strong>{{ player.playerName }}</strong></td>
              <td>{{ player.totalRounds }}</td>
              <td class="player-win-text">{{ player.playerWins }}</td>
              <td class="dealer-win-text">{{ player.dealerWins }}</td>
              <td>
                <button @click.stop="blockPlayer(player.playerName)" class="block-btn">Block</button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-else class="muted">
          No player history found.
        </div>
      </div>
    </div>

    <!-- Player Modal -->
    <div v-if="showModal" class="modal-overlay" @click.self="closeModal">
      <div class="modal-content card">
        <div class="modal-header">
          <h2>📊 {{ selectedPlayer ? selectedPlayer.playerName : 'Loading...' }} Statistics</h2>
          <button @click="closeModal" class="close-btn">&times;</button>
        </div>

        <div v-if="loadingPlayer" class="modal-loading">
          <div class="spinner"></div>
          <p>Fetching detailed player data...</p>
        </div>

        <div v-else-if="selectedPlayer" class="modal-body">
          <!-- Summary Cards -->
          <div class="overview grid">
            <div class="card stat-card">
              <div class="stat-label">Total Rounds</div>
              <div class="stat-value">{{ selectedPlayer.totalRounds }}</div>
            </div>
            <div class="card stat-card player-win">
              <div class="stat-label">Player Win Rate</div>
              <div class="stat-value">{{ selectedPlayer.winRate.toFixed(1) }}%</div>
              <div class="stat-rate">{{ selectedPlayer.playerWins }} wins</div>
            </div>
            <div class="card stat-card dealer-win">
              <div class="stat-label">Dealer Win Rate</div>
              <div class="stat-value">{{ selectedPlayer.lossRate.toFixed(1) }}%</div>
              <div class="stat-rate">{{ selectedPlayer.dealerWins }} losses</div>
            </div>
            <div class="card stat-card" :class="selectedPlayer.netProfit >= 0 ? 'player-win' : 'dealer-win'">
              <div class="stat-label">Net Profit (Player)</div>
              <div class="stat-value" :class="selectedPlayer.netProfit >= 0 ? 'player-win-text' : 'dealer-win-text'">
                {{ selectedPlayer.netProfit >= 0 ? '+' : '' }}{{ selectedPlayer.netProfit }}
              </div>
              <div class="stat-rate">items</div>
            </div>
          </div>

          <!-- Items Breakdown -->
          <div class="breakdown-section">
            <h3>📦 Item Breakdown (Bars In / Bars Out)</h3>
            <table class="compact-table">
              <thead>
                <tr>
                  <th>Item Name</th>
                  <th>Total Bet (In)</th>
                  <th>Total Payout (Out)</th>
                  <th>Net (Player Profit)</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in selectedPlayer.byItem" :key="item.itemName">
                  <td><strong>{{ item.itemName }}</strong></td>
                  <td class="dealer-win-text">{{ item.betIn }}</td>
                  <td class="player-win-text">{{ item.payoutOut }}</td>
                  <td :class="item.net >= 0 ? 'player-win-text' : 'dealer-win-text'">
                    {{ item.net >= 0 ? '+' : '' }}{{ item.net }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- Game Breakdown -->
          <div class="breakdown-section">
            <h3>🎮 Game Breakdown</h3>
            <table class="compact-table">
              <thead>
                <tr>
                  <th>Game</th>
                  <th>Rounds</th>
                  <th>Wins</th>
                  <th>Losses</th>
                  <th>Win Rate</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="game in selectedPlayer.byGame" :key="game.game">
                  <td><strong>{{ game.game }}</strong></td>
                  <td>{{ game.totalRounds }}</td>
                  <td class="player-win-text">{{ game.wins }}</td>
                  <td class="dealer-win-text">{{ game.losses }}</td>
                  <td>{{ game.winRate.toFixed(1) }}%</td>
                </tr>
              </tbody>
            </table>
          </div>
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
  max-width: 1400px;
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
  flex-wrap: wrap;
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

.block-btn {
  background: var(--player);
  color: white;
  border: none;
  padding: 6px 12px;
  border-radius: 6px;
  font-size: 12px;
  cursor: pointer;
}

.block-btn:hover { opacity: 0.8; }

.clickable-row {
  cursor: pointer;
  transition: background 0.2s;
}

.clickable-row:hover {
  background: rgba(255, 255, 255, 0.05);
}

.add-block {
  display: flex;
  gap: 12px;
}

.add-block input {
  flex: 1;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 10px;
  color: white;
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

.compact-table th, .compact-table td {
  padding: 8px 12px;
  font-size: 13px;
}

.muted {
  text-align: center;
  padding: 40px;
  color: var(--muted);
}

.error {
  background: rgba(248, 113, 113, 0.1);
  border-color: var(--player);
  color: var(--player);
}

/* Modal Styles */
.modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.8);
  backdrop-filter: blur(4px);
  display: flex;
  justify-content: center;
  align-items: center;
  z-index: 1000;
}

.modal-content {
  width: 90%;
  max-width: 800px;
  max-height: 90vh;
  overflow-y: auto;
  position: relative;
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
  border-bottom: 1px solid var(--border);
  padding-bottom: 16px;
}

.modal-header h2 {
  margin: 0;
}

.close-btn {
  background: transparent;
  border: none;
  color: var(--muted);
  font-size: 28px;
  cursor: pointer;
  line-height: 1;
}

.close-btn:hover { color: white; }

.modal-loading {
  text-align: center;
  padding: 40px;
}

.breakdown-section {
  margin-top: 32px;
}

.breakdown-section h3 {
  font-size: 18px;
  margin-bottom: 16px;
  color: var(--accent);
}

.spinner {
  width: 40px;
  height: 40px;
  border: 4px solid rgba(255, 255, 255, 0.1);
  border-left-color: var(--accent);
  border-radius: 50%;
  animation: spin 1s linear infinite;
  margin: 0 auto 16px;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
