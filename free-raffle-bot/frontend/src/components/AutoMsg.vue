<script setup>
import { ref, watch, onMounted, onUnmounted } from 'vue';

const props = defineProps({
  state: Object
});

const emit = defineEmits(['refresh']);

const phrase = ref("");
const minutes = ref(10);
const countdown = ref("");
let timerInterval = null;

const syncFromState = () => {
  if (props.state.autoMsgPhrase !== undefined) phrase.value = props.state.autoMsgPhrase;
  if (props.state.autoMsgMinutes) minutes.value = props.state.autoMsgMinutes;
};

const updateCountdown = () => {
  if (!props.state.autoMsgEnabled || !props.state.nextAutoMsgAt) {
    countdown.value = "";
    return;
  }

  const next = new Date(props.state.nextAutoMsgAt).getTime();
  const now = new Date().getTime();
  const diff = next - now;

  if (diff <= 0) {
    countdown.value = "Shouting...";
    return;
  }

  const m = Math.floor(diff / 60000);
  const s = Math.floor((diff % 60000) / 1000);
  countdown.value = `Next msg in ${m}m ${s}s`;
};

watch(() => props.state.autoMsgPhrase, syncFromState);
watch(() => props.state.autoMsgMinutes, syncFromState);
watch(() => props.state.nextAutoMsgAt, updateCountdown);
watch(() => props.state.autoMsgEnabled, updateCountdown);

onMounted(() => {
  syncFromState();
  timerInterval = setInterval(updateCountdown, 1000);
});

onUnmounted(() => {
  if (timerInterval) clearInterval(timerInterval);
});

const call = async (name, ...args) => {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return await window.go.main.App[name](...args);
  }
};

const saveConfig = async () => {
  await call('SetAutoMsgConfig', phrase.value, parseInt(minutes.value));
  emit('refresh');
};

const toggleAuto = async () => {
  await call('ToggleAutoMsg', !props.state.autoMsgEnabled);
  emit('refresh');
};
</script>

<template>
  <div class="card">
    <div class="row" style="justify-content: space-between; margin-bottom: 8px;">
      <div style="display:flex; flex-direction:column; gap:2px;">
        <h3 style="margin:0">Auto Msg (Timer)</h3>
        <span v-if="state.autoMsgEnabled && countdown" class="countdown">{{ countdown }}</span>
      </div>
      <button :class="state.autoMsgEnabled ? 'stop' : 'ok-btn'" @click="toggleAuto">
        {{ state.autoMsgEnabled ? 'Disable Msg' : 'Enable Msg' }}
      </button>
    </div>
    <p class="sub">Sends a periodic message to the room. Use <code>[prize]</code> to auto-insert the raffle prize name.</p>
    
    <div class="row" style="margin-top: 12px; align-items: flex-end;">
      <div style="flex: 1; display:flex; flex-direction:column; gap:4px;">
        <label style="font-size:12px; color:var(--muted)">Message</label>
        <input type="text" v-model="phrase" placeholder="Type your custom message here..." style="width:100%">
      </div>
      <div style="display:flex; flex-direction:column; gap:4px;">
        <label style="font-size:12px; color:var(--muted)">Interval (mins)</label>
        <input type="number" v-model="minutes" min="1">
      </div>
      <button class="alt" @click="saveConfig">Save & Apply</button>
    </div>
  </div>
</template>

<style scoped>
.ok-btn { background: var(--ok); color: #fff; }
.countdown {
  font-size: 12px;
  color: #62c370;
  font-weight: bold;
}
</style>
