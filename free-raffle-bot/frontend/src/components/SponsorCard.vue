<script setup>
import { ref, watch } from 'vue';

const props = defineProps({
  state: Object
});
const emit = defineEmits(['refresh']);

const sponsorEnabled = ref(false);
const sponsorName = ref('');
const sponsorRoomName = ref('');
const sponsorImageDataUrl = ref('');
const sponsorImageFileName = ref('');

watch(() => props.state, (newState) => {
  sponsorEnabled.value = newState.sponsorEnabled;
  sponsorName.value = newState.sponsorName || '';
  sponsorRoomName.value = newState.sponsorRoomName || '';
}, { immediate: true, deep: true });

async function call(name, ...args) {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return window.go.main.App[name](...args);
  }
  console.error(`Backend method ${name} not available.`);
  throw new Error(`Backend method ${name} is not available.`);
}

function handleSponsorImageChange(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = (e) => {
    sponsorImageDataUrl.value = e.target.result;
    sponsorImageFileName.value = file.name;
    const preview = document.getElementById('sponsorCardImagePreview');
    if (preview) {
      preview.src = e.target.result;
      preview.style.display = 'block';
    }
    const meta = document.getElementById('sponsorCardImageMeta');
    if (meta) meta.textContent = `Selected: ${file.name}`;
  };
  reader.readAsDataURL(file);
}

async function saveSponsorCfg() {
  try {
    await call('SetRaffleSponsorConfig', sponsorEnabled.value, sponsorName.value, sponsorRoomName.value, sponsorImageDataUrl.value, sponsorImageFileName.value);
    // Clear local buffers after save to prevent re-uploading the same data on next save if not changed
    sponsorImageDataUrl.value = '';
    sponsorImageFileName.value = '';
    emit('refresh');
  } catch (err) {
    alert(`Could not save Sponsor config: ${err}`);
  }
}
</script>

<template>
  <div class="card orange-card">
    <h2 style="margin: 0 0 10px 0;">💎 Sponsorship Information</h2>
    <p class="sub" style="margin-bottom: 12px; color: rgba(255,255,255,0.8);">Credit sponsors and display their room photo on the Discord raffle post.</p>

    <div style="margin-bottom: 12px;">
      <label style="display:flex;align-items:center;gap:8px;cursor:pointer;justify-content:flex-start; font-weight: bold;">
        <input v-model="sponsorEnabled" type="checkbox"> Enable Sponsorship
      </label>
    </div>

    <template v-if="sponsorEnabled">
      <div class="row" style="margin-bottom: 8px;">
        <label for="sponsorCardName" style="min-width: 140px;">Sponsor Name</label>
        <input v-model="sponsorName" id="sponsorCardName" type="text" placeholder="e.g. Dubbo" style="flex: 1;" />
      </div>

      <div class="row" style="margin-bottom: 8px;">
        <label for="sponsorCardRoom" style="min-width: 140px;">Sponsor Room</label>
        <input v-model="sponsorRoomName" id="sponsorCardRoom" type="text" placeholder="e.g. Rare Trade [1]" style="flex: 1;" />
      </div>

      <div class="row" style="margin-bottom: 12px; align-items:flex-start;">
        <label for="sponsorCardImage" style="min-width: 140px;">Sponsor Photo</label>
        <div style="flex: 1;">
          <input id="sponsorCardImage" type="file" accept="image/*" @change="handleSponsorImageChange" />
          <div id="sponsorCardImageMeta" class="muted-light" style="margin-top:6px; font-size: 12px;">No sponsor image selected.</div>
          <img id="sponsorCardImagePreview" class="raffle-hero-preview" style="margin-top:8px; display:none; border: 2px solid rgba(255,255,255,0.2);" alt="Sponsor room preview" />
        </div>
      </div>
    </template>

    <div class="row" style="margin-top: 12px; border-top: 1px solid rgba(255,255,255,0.15); padding-top: 12px;">
      <button @click="saveSponsorCfg" class="sponsor-save-btn">Update Sponsor Info</button>
    </div>
  </div>
</template>

<style scoped>
.orange-card {
  background: linear-gradient(135deg, #e67e22, #d35400) !important;
  border: 1px solid rgba(255,255,255,0.3) !important;
  color: white !important;
}

.orange-card h2 {
  color: white;
}

.muted-light {
  color: rgba(255,255,255,0.7);
}

.sponsor-save-btn {
  background: white;
  color: #d35400;
  border: 0;
  font-weight: 800;
}

.sponsor-save-btn:hover {
  background: #fdf2e9;
}

input[type="text"] {
    background: rgba(255,255,255,0.15);
    border: 1px solid rgba(255,255,255,0.3);
    color: white;
}

input[type="text"]::placeholder {
    color: rgba(255,255,255,0.5);
}
</style>
