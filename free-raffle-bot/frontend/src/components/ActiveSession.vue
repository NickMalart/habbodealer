<script setup>
import { ref, computed } from 'vue';

const props = defineProps({
  state: Object
});
const emit = defineEmits(['refresh']);

const manualParticipantName = ref('');
const manualParticipantBets = ref(0);
const manualParticipantTickets = ref(0);
const editingParticipantKey = ref('');

const sessionMeta = computed(() => {
  if (!props.state.currentSession) {
    return 'No active session';
  }
  const s = props.state.currentSession;
  const endInfo = s.scheduledEndAt ? `, ends ${new Date(s.scheduledEndAt).toLocaleString()}` : ', no end set';
  return `Session #${s.id} | Started: ${new Date(s.startedAt).toLocaleString()}${endInfo}`;
});

async function call(name, ...args) {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return window.go.main.App[name](...args);
  }
  console.error(`Backend method ${name} not available.`);
  throw new Error(`Backend method ${name} is not available.`);
}

function clearManualParticipant() {
  manualParticipantName.value = '';
  manualParticipantBets.value = 0;
  manualParticipantTickets.value = 0;
  editingParticipantKey.value = '';
}

async function saveManualParticipant() {
  if (!manualParticipantName.value.trim()) {
    alert('Enter a username first.');
    return;
  }
  try {
    await call('UpsertManualParticipant', manualParticipantName.value, manualParticipantBets.value, manualParticipantTickets.value);
    editingParticipantKey.value = manualParticipantName.value.trim().toLowerCase();
    emit('refresh');
  } catch (err) {
    alert(`Could not save manual participant: ${err}`);
  }
}

function editParticipant(p) {
  manualParticipantName.value = p.username;
  manualParticipantBets.value = p.betCount;
  manualParticipantTickets.value = p.tickets;
  editingParticipantKey.value = p.username.trim().toLowerCase();
}

async function removeParticipant(username) {
  if (!confirm(`Remove ${username} from this raffle session?`)) return;
  try {
    await call('RemoveParticipantFromCurrentSession', username);
    if (editingParticipantKey.value === username.trim().toLowerCase()) {
      clearManualParticipant();
    }
    emit('refresh');
  } catch (err) {
    alert(`Could not remove participant: ${err}`);
  }
}

</script>

<template>
  <div class="card">
    <div v-if="state.currentSession">
      <div class="row" style="justify-content: space-between;">
        <div>
          <h2 style="margin: 0;">{{ state.currentSession.raffleName || 'Current Session' }}</h2>
          <p class="sub" style="margin-top: 4px;">
            Prize: {{ state.currentSession.prizeName || 'N/A' }} (x{{ state.currentSession.prizeQty || 1 }})
          </p>
        </div>
        <span class="muted">{{ sessionMeta }}</span>
      </div>
    </div>
    <div v-else>
       <h2 style="margin: 0;">Current Session</h2>
       <p class="sub">No active session.</p>
    </div>

    <div class="card" style="margin-top:12px;padding:12px;background:rgba(255,255,255,0.03);">
      <div class="row" style="justify-content:space-between;align-items:flex-start;">
        <div>
          <div style="font-weight:700;margin-bottom:4px;">Manual Participant</div>
          <div class="muted">Add someone to the active raffle or edit an existing player.</div>
        </div>
        <button class="alt" @click="clearManualParticipant">Clear</button>
      </div>
      <div class="row" style="margin-top:10px;">
        <label for="manualParticipantName" style="min-width:80px;">Player</label>
        <input v-model="manualParticipantName" id="manualParticipantName" type="text" placeholder="Username" />
        <label for="manualParticipantBets">Rounds Played</label>
        <input v-model.number="manualParticipantBets" id="manualParticipantBets" type="number" min="0" />
        <label for="manualParticipantTickets">Tickets</label>
        <input v-model.number="manualParticipantTickets" id="manualParticipantTickets" type="number" min="0" />
        <button @click="saveManualParticipant" :disabled="!state.currentSession">Add / Update Player</button>
      </div>
    </div>
    <table>
      <thead>
        <tr>
          <th>Player</th>
          <th>Rounds Played</th>
          <th>Tickets</th>
          <th>Last Bet</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="!state.currentSession || !state.currentSession.participants || state.currentSession.participants.length === 0">
          <td colspan="5" class="muted">No participants yet.</td>
        </tr>
        <tr v-for="p in state.currentSession?.participants" :key="p.username">
          <td>{{ p.username }}</td>
          <td>{{ p.betCount }}</td>
          <td>{{ p.tickets }}</td>
          <td>{{ new Date(p.lastBet).toLocaleString() }}</td>
          <td style="display:flex;gap:6px;">
            <button class="alt" @click="editParticipant(p)">Edit</button>
            <button class="stop" @click="removeParticipant(p.username)">Remove</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
