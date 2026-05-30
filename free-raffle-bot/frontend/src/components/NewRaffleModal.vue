<script setup>
import { ref, onMounted } from 'vue';

const emit = defineEmits(['close', 'create-raffle']);

const name = ref('');
const prize = ref('');
const qty = ref(1);
const startAt = ref('');
const endAt = ref('');
const heroImageDataUrl = ref('');
const heroImageFileName = ref('');

const sponsorEnabled = ref(false);
const sponsorName = ref('');
const sponsorRoomName = ref('');

onMounted(() => {
  const start = new Date();
  const end = new Date(start.getTime() + 60 * 60 * 1000);
  const toLocalField = (d) => {
    const p = (n) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
  };
  startAt.value = toLocalField(start);
  endAt.value = toLocalField(end);
});

function handleHeroImageChange(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = (e) => {
    heroImageDataUrl.value = e.target.result;
    heroImageFileName.value = file.name;
    document.getElementById('modalHeroImagePreview').src = e.target.result;
    document.getElementById('modalHeroImagePreview').style.display = 'block';
  };
  reader.readAsDataURL(file);
}

function createRaffle() {
  const startISO = startAt.value ? new Date(startAt.value).toISOString() : '';
  const endISO = endAt.value ? new Date(endAt.value).toISOString() : '';

  emit('create-raffle', {
    name: name.value,
    prize: prize.value,
    qty: qty.value,
    heroDataUrl: heroImageDataUrl.value,
    heroFileName: heroImageFileName.value,
    startAt: startISO,
    endAt: endISO,
    sponsorEnabled: sponsorEnabled.value,
    sponsorName: sponsorName.value,
    sponsorRoom: sponsorRoomName.value,
  });
}
</script>

<template>
  <div class="modal-backdrop">
    <div class="modal-content">
      <h2>Create New Raffle</h2>

      <div class="form-grid">
        <label for="raffleName">Raffle Name</label>
        <input v-model="name" id="raffleName" type="text" placeholder="Weekend Raffle" />

        <label for="rafflePrize">Prize Name</label>
        <input v-model="prize" id="rafflePrize" type="text" placeholder="Purple Dragon Lamp" />

        <label for="rafflePrizeQty">Quantity</label>
        <input v-model.number="qty" id="rafflePrizeQty" type="number" min="1" />

        <label for="startAt">Start Time</label>
        <input v-model="startAt" id="startAt" type="datetime-local" />

        <label for="endAt">End Time</label>
        <input v-model="endAt" id="endAt" type="datetime-local" />

        <label for="heroImage">Hero Image</label>
        <div>
          <input id="heroImage" type="file" accept="image/*" @change="handleHeroImageChange" />
          <img id="modalHeroImagePreview" class="raffle-hero-preview" style="margin-top:8px;display:none;" alt="Raffle hero preview" />
        </div>

        <div style="grid-column: 1 / span 2; margin-top: 12px; padding-top: 12px; border-top: 1px solid rgba(255,255,255,0.05);">
          <label style="display:flex;align-items:center;gap:8px;cursor:pointer;justify-content:flex-start;">
            <input v-model="sponsorEnabled" type="checkbox"> 💎 Add Sponsorship Info
          </label>
        </div>

        <template v-if="sponsorEnabled">
          <label for="sponsorName">Sponsor Name</label>
          <input v-model="sponsorName" id="sponsorName" type="text" placeholder="e.g. Dubbo" />

          <label for="sponsorRoom">Sponsor Room</label>
          <input v-model="sponsorRoomName" id="sponsorRoom" type="text" placeholder="e.g. Rare Trade [1]" />
        </template>
      </div>

      <div class="modal-actions">
        <button class="alt" @click="$emit('close')">Cancel</button>
        <button @click="createRaffle">Create and Start Raffle</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.modal-backdrop {
  position: fixed;
  top: 0;
  left: 0;
  width: 100vw;
  height: 100vh;
  background-color: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal-content {
  background: var(--panel);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 14px;
  padding: 24px;
  width: 90%;
  max-width: 600px;
  max-height: 90vh;
  overflow-y: auto;
}

.modal-content h2 {
  margin-top: 0;
}

.form-grid {
  display: grid;
  grid-template-columns: 120px 1fr;
  gap: 16px;
  align-items: center;
}

.form-grid label {
  text-align: right;
  color: var(--muted);
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 24px;
  border-top: 1px solid rgba(255,255,255,0.1);
  padding-top: 16px;
}

.raffle-hero-preview {
    max-width: 150px;
}
</style>
