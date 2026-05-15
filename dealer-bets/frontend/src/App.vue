<template>
  <div class="container">
    <header>
      <h1>Dealer Bets</h1>
      <div class="status" :class="{ connected: state.connected }">
        {{ state.connected ? 'G-Earth Connected' : 'Waiting for G-Earth...' }}
      </div>
    </header>

    <div class="main-content">
      <div class="bet-form">
        <h2>Add New Bet</h2>
        <div class="form-group">
          <input v-model="newBet.player" placeholder="Player Name" />
          <input v-model.number="newBet.amount" type="number" placeholder="Amount" />
          <select v-model="newBet.game">
            <option value="Poker">Poker</option>
            <option value="Dice">Dice</option>
            <option value="Uo7">Uo7</option>
          </select>
          <button @click="addBet" :disabled="!newBet.player || !newBet.amount">Add Bet</button>
        </div>
      </div>

      <div class="bet-list">
        <h2>Active Bets</h2>
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Player</th>
              <th>Amount</th>
              <th>Game</th>
              <th>Status</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="bet in state.bets" :key="bet.id">
              <td>#{{ bet.id }}</td>
              <td>{{ bet.player }}</td>
              <td>{{ bet.amount }}</td>
              <td>{{ bet.game }}</td>
              <td :class="bet.status.toLowerCase()">{{ bet.status }}</td>
              <td>
                <button class="win" @click="updateStatus(bet.id, 'Won')">Win</button>
                <button class="loss" @click="updateStatus(bet.id, 'Lost')">Loss</button>
              </td>
            </tr>
            <tr v-if="state.bets.length === 0">
              <td colspan="6" style="text-align: center;">No active bets</td>
            </tr>
          </tbody>
        </table>
        <button class="clear" @click="clearBets" v-if="state.bets.length > 0">Clear All</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { reactive, onMounted } from 'vue'

const state = reactive({
  connected: false,
  bets: []
})

const newBet = reactive({
  player: '',
  amount: null,
  game: 'Poker'
})

const addBet = () => {
  window.go.main.App.AddBet(newBet.player, newBet.amount, newBet.game)
  newBet.player = ''
  newBet.amount = null
}

const updateStatus = (id, status) => {
  window.go.main.App.UpdateBetStatus(id, status)
}

const clearBets = () => {
  if (confirm('Clear all bets?')) {
    window.go.main.App.ClearBets()
  }
}

onMounted(() => {
  // Get initial state
  window.go.main.App.GetState().then(res => {
    state.connected = res.connected
    state.bets = res.bets
  })

  // Listen for updates
  window.runtime.EventsOn('update', (res) => {
    state.connected = res.connected
    state.bets = res.bets
  })
})
</script>

<style scoped>
.container {
  padding: 20px;
}
header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid #30363d;
  padding-bottom: 10px;
  margin-bottom: 20px;
}
.status {
  padding: 5px 15px;
  border-radius: 20px;
  background: #ff4444;
  font-size: 14px;
}
.status.connected {
  background: #00c851;
}
.form-group {
  display: flex;
  gap: 10px;
  margin-bottom: 30px;
}
input, select, button {
  padding: 8px 12px;
  border-radius: 4px;
  border: 1px solid #30363d;
  background: #0d1117;
  color: white;
}
button {
  background: #21262d;
  cursor: pointer;
}
button:hover {
  background: #30363d;
}
button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
table {
  width: 100%;
  border-collapse: collapse;
}
th, td {
  text-align: left;
  padding: 12px;
  border-bottom: 1px solid #30363d;
}
.won { color: #00c851; font-weight: bold; }
.lost { color: #ff4444; font-weight: bold; }
.win { background: #007e33; color: white; border: none; }
.loss { background: #cc0000; color: white; border: none; }
.clear { margin-top: 20px; background: #30363d; }
</style>
