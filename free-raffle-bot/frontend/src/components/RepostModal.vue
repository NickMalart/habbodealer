<script setup>
import { ref, onMounted } from 'vue';

const props = defineProps({
  session: Object
});
const emit = defineEmits(['close', 'repost']);

const heroImageDataUrl = ref('');
const heroImageFileName = ref('');
const sponsorImageDataUrl = ref('');
const sponsorImageFileName = ref('');

function handleHeroImageChange(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = (e) => {
    heroImageDataUrl.value = e.target.result;
    heroImageFileName.value = file.name;
    document.getElementById('repostHeroPreview').src = e.target.result;
    document.getElementById('repostHeroPreview').style.display = 'block';
  };
  reader.readAsDataURL(file);
}

function handleSponsorImageChange(event) {
  const file = event.target.files[0];
  if (!file) return;
  const reader = new FileReader();
  reader.onload = (e) => {
    sponsorImageDataUrl.value = e.target.result;
    sponsorImageFileName.value = file.name;
    document.getElementById('repostSponsorPreview').src = e.target.result;
    document.getElementById('repostSponsorPreview').style.display = 'block';
  };
  reader.readAsDataURL(file);
}

function repost() {
  emit('repost', {
    dbId: props.session.dbId,
    heroDataUrl: heroImageDataUrl.value,
    heroFileName: heroImageFileName.value,
    sponsorDataUrl: sponsorImageDataUrl.value,
    sponsorFileName: sponsorImageFileName.value,
  });
}
</script>

<template>
  <div class="modal-backdrop">
    <div class="modal-content">
      <h2>Repost Raffle #{{ session.id }}</h2>
      <p class="sub" style="margin-bottom: 20px;">
        Reposting <strong>{{ session.raffleName }}</strong> ({{ session.prizeName }}). 
        You can keep the existing images or upload new ones below.
      </p>

      <div class="form-grid">
        <label for="repostHeroImage">New Prize Photo</label>
        <div>
          <input id="repostHeroImage" type="file" accept="image/*" @change="handleHeroImageChange" />
          <div class="muted-small">Leave empty to keep current prize photo.</div>
          <img id="repostHeroPreview" class="raffle-hero-preview" style="margin-top:8px;display:none;" alt="New hero preview" />
        </div>

        <template v-if="session.sponsorEnabled || true">
            <label for="repostSponsorImage">New Sponsor Photo</label>
            <div>
              <input id="repostSponsorImage" type="file" accept="image/*" @change="handleSponsorImageChange" />
              <div class="muted-small">Leave empty to keep current sponsor photo.</div>
              <img id="repostSponsorPreview" class="raffle-hero-preview" style="margin-top:8px;display:none;" alt="New sponsor preview" />
            </div>
        </template>
      </div>

      <div class="modal-actions">
        <button class="alt" @click="$emit('close')">Cancel</button>
        <button @click="repost">Send Repost to Discord</button>
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
  background-color: rgba(0, 0, 0, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 2000;
}

.modal-content {
  background: var(--panel);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 14px;
  padding: 24px;
  width: 90%;
  max-width: 550px;
  max-height: 90vh;
  overflow-y: auto;
}

.form-grid {
  display: grid;
  grid-template-columns: 140px 1fr;
  gap: 20px;
  align-items: flex-start;
}

.form-grid label {
  text-align: right;
  color: var(--muted);
  font-weight: 600;
  padding-top: 8px;
}

.muted-small {
    font-size: 11px;
    color: var(--muted);
    margin-top: 4px;
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
    max-width: 180px;
    border-radius: 8px;
}
</style>
