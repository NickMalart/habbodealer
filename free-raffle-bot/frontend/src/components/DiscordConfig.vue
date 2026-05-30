<script setup>
import { ref, watch } from 'vue';

const props = defineProps({
  state: Object
});
const emit = defineEmits(['refresh']);

const raffleName = ref('');
const rafflePrizeName = ref('');
const rafflePrizeQty = ref(1);
const raffleAutoUpdate = ref(true);
const sponsorEnabled = ref(false);
const sponsorName = ref('');
const sponsorRoomName = ref('');
const heroImageDataUrl = ref('');
const heroImageFileName = ref('');
const winnerProofDataUrl = ref('');
const winnerProofFileName = ref('');

watch(() => props.state, (newState) => {
  raffleName.value = newState.raffleName || '';
  rafflePrizeName.value = newState.rafflePrizeName || '';
  rafflePrizeQty.value = newState.rafflePrizeQty || 1;
  raffleAutoUpdate.value = newState.raffleAutoUpdate;
  sponsorEnabled.value = newState.sponsorEnabled;
  sponsorName.value = newState.sponsorName || '';
  sponsorRoomName.value = newState.sponsorRoomName || '';
  // hero image is handled via file input
}, { immediate: true, deep: true });

async function call(name, ...args) {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return window.go.main.App[name](...args);
  }
  console.error(`Backend method ${name} not available.`);
  throw new Error(`Backend method ${name} is not available.`);
}

function handleHeroImageChange(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = (e) => {
    heroImageDataUrl.value = e.target.result;
    heroImageFileName.value = file.name;
    document.getElementById('heroImagePreview').src = e.target.result;
    document.getElementById('heroImagePreview').style.display = 'block';
    document.getElementById('heroImageMeta').textContent = `Selected: ${file.name}`;
  };
  reader.readAsDataURL(file);
}

function handleWinnerProofImageChange(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = (e) => {
    winnerProofDataUrl.value = e.target.result;
    winnerProofFileName.value = file.name;
    document.getElementById('winnerProofPreview').src = e.target.result;
    document.getElementById('winnerProofPreview').style.display = 'block';
    document.getElementById('winnerProofMeta').textContent = `Selected proof photo: ${file.name}`;
  };
  reader.readAsDataURL(file);
}

async function saveDiscordCfg() {
  try {
    await call('SetRaffleSponsorConfig', sponsorEnabled.value, sponsorName.value, sponsorRoomName.value);
    await call('SetRaffleDiscordConfig', raffleName.value, rafflePrizeName.value, rafflePrizeQty.value, heroImageDataUrl.value, heroImageFileName.value, raffleAutoUpdate.value);
    emit('refresh');
  } catch (err) {
    alert(`Could not save Discord config: ${err}`);
  }
}

async function postOrUpdateRaffleWebhook() {
  try {
    await saveDiscordCfg(); // Save before posting
    const result = await call('PostOrUpdateRaffleWebhook');
    if (result !== 'ok') {
      alert(`Discord webhook response: ${result}`);
    } else {
      heroImageDataUrl.value = '';
      heroImageFileName.value = '';
      document.getElementById('heroImage').value = '';
      document.getElementById('heroImagePreview').style.display = 'none';
      document.getElementById('heroImageMeta').textContent = 'No hero image selected.';
    }
    emit('refresh');
  } catch (err) {
    alert(`Could not post raffle webhook: ${err}`);
  }
}

async function repostRaffleWebhook() {
  if (!confirm('This will send a brand-new Discord message (the old one stays). Continue?')) return;
  try {
    await saveDiscordCfg(); // Save before posting
    const result = await call('RepostRaffleWebhook');
    if (result !== 'ok') {
      alert(`Repost response: ${result}`);
    }
    emit('refresh');
  } catch (err) {
    alert(`Could not repost: ${err}`);
  }
}

async function drawWinner() {
  try {
    const result = await call('DrawWinnerForCurrentSession');
    if (result && typeof result === 'string' && result.toLowerCase() !== 'ok') {
      alert(result);
    }
    emit('refresh');
  } catch (err) {
    alert(`Could not draw winner: ${err}`);
  }
}

async function postWinnerProof() {
  if (!winnerProofDataUrl.value) {
    alert('Please upload a winner proof photo first.');
    return;
  }
  try {
    const result = await call('PostWinnerProofForCurrentSession', winnerProofDataUrl.value, winnerProofFileName.value);
    if (result !== 'ok') {
      alert(`Winner proof response: ${result}`);
    } else {
      alert('Winner proof posted to Discord.');
    }
    emit('refresh');
  } catch (err) {
    alert(`Could not post winner proof: ${err}`);
  }
}
</script>

<template>
  <div class="card">
    <h2 style="margin: 0 0 10px 0;">Discord Raffle Post</h2>
    <p class="sub" style="margin-bottom: 12px;">Set raffle details, upload a hero image, and post/update the live ticket tracker.</p>

    <div class="row" style="margin-bottom: 8px;">
      <label for="raffleName" style="min-width: 140px;">Raffle Name</label>
      <input v-model="raffleName" id="raffleName" type="text" placeholder="Weekend Raffle" />
    </div>

    <div class="row" style="margin-bottom: 8px;">
      <label for="rafflePrize" style="min-width: 140px;">Prize Name</label>
      <input v-model="rafflePrizeName" id="rafflePrize" type="text" placeholder="Purple Dragon Lamp" />
      <label for="rafflePrizeQty" style="min-width: 80px;">Qty</label>
      <input v-model.number="rafflePrizeQty" id="rafflePrizeQty" type="number" min="1" style="width:90px;" />
    </div>

    <div class="row" style="margin-bottom: 8px;align-items:flex-start;">
      <label for="heroImage" style="min-width: 140px;">Hero Image</label>
      <div>
        <input id="heroImage" type="file" accept="image/*" @change="handleHeroImageChange" />
        <div id="heroImageMeta" class="muted" style="margin-top:6px;">No hero image selected.</div>
        <img id="heroImagePreview" class="raffle-hero-preview" style="margin-top:8px;display:none;" alt="Raffle hero preview" />
      </div>
    </div>

    <div class="row" style="margin-top: 10px;">
      <label style="display:flex;align-items:center;gap:6px;cursor:pointer;">
        <input v-model="raffleAutoUpdate" type="checkbox" id="raffleAutoUpdateChk"> Auto-update Discord tracker
      </label>
    </div>

    <div class="row" style="margin-top: 12px;">
      <button class="alt" @click="saveDiscordCfg">Save Settings</button>
      <button @click="postOrUpdateRaffleWebhook">Post / Update Raffle</button>
      <button class="alt" @click="repostRaffleWebhook">Repost to Discord</button>
      <button class="alt" @click="drawWinner">Draw Winner</button>
    </div>

    <div class="row" style="margin-top: 10px;" v-if="state.currentSession && state.currentSession.winnerName">
      <div class="winner-note">
        Winner: {{ state.currentSession.winnerName }} | Odds: {{ state.currentSession.winnerOdds || 'n/a' }}
      </div>
    </div>

    <div v-if="state.currentSession && state.currentSession.winnerName" style="margin-top:12px;border-top:1px solid rgba(255,255,255,0.12);padding-top:12px;">
      <div class="row" style="margin-bottom:8px;align-items:flex-start;">
        <label for="winnerProofImage" style="min-width: 140px;">Winner Proof</label>
        <div>
          <input id="winnerProofImage" type="file" accept="image/*" @change="handleWinnerProofImageChange" />
          <div id="winnerProofMeta" class="muted" style="margin-top:6px;">Upload proof photo.</div>
          <img id="winnerProofPreview" class="raffle-hero-preview" style="margin-top:8px;display:none;" alt="Winner proof preview" />
        </div>
      </div>
      <div class="row">
        <button class="alt" @click="postWinnerProof">Post Winner Proof</button>
      </div>
    </div>
  </div>
</template>
