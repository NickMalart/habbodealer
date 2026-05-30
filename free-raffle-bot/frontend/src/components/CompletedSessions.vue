<script setup>
import { ref, computed } from 'vue';
import RepostModal from './RepostModal.vue';

const props = defineProps({
  state: Object
});
const emit = defineEmits(['refresh']);

const proofDataUrl = ref('');
const proofFileName = ref('');
const uploadingId = ref(null);

const showRepostModal = ref(false);
const selectedSession = ref(null);

const completedSessions = computed(() => {
  const all = [...props.state.sessions];
  if (props.state.currentSession) {
    all.push(props.state.currentSession);
  }
  return all.filter(s => s.winnerName).sort((a, b) => b.id - a.id);
});

async function call(name, ...args) {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return window.go.main.App[name](...args);
  }
}

function openRepostModal(session) {
  selectedSession.value = session;
  showRepostModal.value = true;
}

async function handleRepost(data) {
  try {
    const res = await call('RepostSessionWebhook', data.dbId, data.heroDataUrl, data.heroFileName, data.sponsorDataUrl, data.sponsorFileName);
    if (res !== 'ok') {
      alert(`Repost response: ${res}`);
    } else {
      alert('Raffle successfully reposted to Discord!');
    }
    showRepostModal.value = false;
    emit('refresh');
  } catch (err) {
    alert(`Repost error: ${err}`);
  }
}

function handleFileChange(event, sessionId) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = (e) => {
    proofDataUrl.value = e.target.result;
    proofFileName.value = file.name;
    uploadingId.value = sessionId;
  };
  reader.readAsDataURL(file);
}

async function postProof(dbID) {
  if (!proofDataUrl.value || uploadingId.value !== dbID) {
    alert('Please select a photo for this raffle first.');
    return;
  }
  try {
    const result = await call('PostWinnerProofForSession', dbID, proofDataUrl.value, proofFileName.value);
    if (result !== 'ok') {
      alert(`Response: ${result}`);
    } else {
      alert('Winner proof posted to Discord!');
      proofDataUrl.value = '';
      proofFileName.value = '';
      uploadingId.value = null;
      emit('refresh');
    }
  } catch (err) {
    alert(`Error: ${err}`);
  }
}
</script>

<template>
  <div class="card">
    <h2 style="margin: 0 0 10px 0;">Completed Raffles</h2>
    <p class="sub" style="margin-bottom: 12px;">Raffles with winners drawn. Upload a proof photo to patch the Discord post.</p>

    <div v-if="completedSessions.length === 0" class="muted">
      No completed raffles found.
    </div>

    <div v-for="s in completedSessions" :key="s.id" class="card" style="background:rgba(255,255,255,0.02); margin-bottom:10px;">
      <div class="row" style="justify-content: space-between;">
        <div style="text-align:right;">
          <h3 style="margin:0;">#{{ s.id }} - {{ s.raffleName || 'Raffle' }}</h3>
          <div class="muted" style="font-size:13px;">Winner: <strong>{{ s.winnerName }}</strong> | Ended: {{ new Date(s.endedAt || s.winnerDrawnAt).toLocaleString() }}</div>
        </div>
        <button class="alt" @click="openRepostModal(s)">Repost to Discord</button>
      </div>

      <div style="margin-top:10px; border-top:1px solid rgba(255,255,255,0.05); padding-top:10px;">
        <div v-if="s.winnerProofUrl" class="winner-note" style="margin-bottom:8px;">
          Proof already posted: <a :href="s.winnerProofUrl" target="_blank" style="color:inherit;">View Photo</a>
        </div>
        
        <div class="row" style="align-items: flex-start;">
          <div style="flex:1;">
            <label style="display:block; font-size:12px; margin-bottom:4px;" :for="'file-' + s.id">Update Winner Photo</label>
            <input :id="'file-' + s.id" type="file" accept="image/*" @change="e => handleFileChange(e, s.id)" />
            <div v-if="uploadingId === s.id" class="muted" style="margin-top:4px; font-size:12px;">Selected: {{ proofFileName }}</div>
          </div>
          <button class="alt" @click="postProof(s.dbId)" :disabled="uploadingId !== s.id">Post Proof to Discord</button>
        </div>
      </div>
    </div>

    <RepostModal 
      v-if="showRepostModal" 
      :session="selectedSession" 
      @close="showRepostModal = false" 
      @repost="handleRepost" 
    />
  </div>
</template>
