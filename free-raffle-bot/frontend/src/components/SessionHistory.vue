<script setup>
import { ref } from 'vue';

const props = defineProps({
  state: Object
});
const emit = defineEmits(['refresh']);

const showTally = ref(false);
const tallyData = ref(null);
const tallyLoading = ref(false);
const tallyError = ref('');

async function call(name, ...args) {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return window.go.main.App[name](...args);
  }
  console.error(`Backend method ${name} not available.`);
  throw new Error(`Backend method ${name} is not available.`);
}

async function loadTally(dbID) {
  showTally.value = true;
  tallyLoading.value = true;
  tallyError.value = '';
  tallyData.value = null;
  try {
    const data = await call('GetSessionTally', dbID);
    tallyData.value = data;
  } catch (err) {
    tallyError.value = `Error: ${err}`;
  } finally {
    tallyLoading.value = false;
  }
}

async function resumeSession(dbID) {
  try {
    await call('ResumeSession', dbID);
    emit('refresh');
  } catch (err) {
    alert(`Could not resume session: ${err}`);
  }
}

async function deleteSession(dbID) {
  if (!confirm('Are you sure you want to delete this raffle and all its participants?')) return;
  try {
    await call('DeleteSession', dbID);
    emit('refresh');
  } catch (err) {
    alert(`Could not delete session: ${err}`);
  }
}
</script>

<template>
  <div class="card" v-if="state.sessions && state.sessions.length > 0">
    <h2 style="margin: 0 0 10px 0;">Past Sessions</h2>
    <table>
      <thead>
        <tr>
          <th>ID</th>
          <th>Started</th>
          <th>Ended</th>
          <th>Winner</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="s in [...state.sessions].reverse()" :key="s.id">
          <td>#{{ s.id }}</td>
          <td>{{ s.startedAt }}</td>
          <td>{{ s.endedAt }}</td>
          <td>{{ s.winnerName || 'N/A' }}</td>
          <td style="display:flex;gap:4px;">
            <button class="alt" @click="resumeSession(s.dbId)">Resume</button>
            <button class="alt" @click="loadTally(s.dbId)">Tally</button>
            <button class="stop" @click="deleteSession(s.dbId)">Delete</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <div class="card" v-if="showTally">
    <div class="row" style="justify-content: space-between; margin-bottom:8px;">
      <h2 style="margin: 0;">Item Tally <span class="muted" style="font-size:13px;">(Session #{{ tallyData?.sessionDbId }})</span></h2>
      <button class="alt" @click="showTally = false">Close</button>
    </div>
    <p v-if="tallyData" class="muted" style="margin:0 0 8px 0;">
      {{ tallyData.games }} completed game(s) between {{ tallyData.startedAt }} and {{ tallyData.endedAt || 'now' }}
    </p>
    <p v-if="tallyError" class="muted" style="color: var(--warn);">{{ tallyError }}</p>
    <table>
      <thead>
        <tr>
          <th>Item</th>
          <th>Won (in)</th>
          <th>Lost (out)</th>
          <th>Net</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="tallyLoading">
          <td colspan="4" class="muted">Loading...</td>
        </tr>
        <tr v-else-if="!tallyData || !tallyData.items || tallyData.items.length === 0">
          <td colspan="4" class="muted">No item data found for this window.</td>
        </tr>
        <tr v-for="item in tallyData?.items" :key="item.name">
          <td>{{ item.name }}</td>
          <td>{{ item.wonQty }}</td>
          <td>{{ item.lostQty }}</td>
          <td :style="{ color: item.netQty > 0 ? '#4caf50' : item.netQty < 0 ? '#f44336' : '' }">
            {{ item.netQty > 0 ? '+' : '' }}{{ item.netQty }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
