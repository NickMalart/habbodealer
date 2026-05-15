<script setup>
import { ref, watch, onMounted } from 'vue';

const props = defineProps({
  state: Object
});

const emit = defineEmits(['refresh']);

const phrase = ref("Current Raffle: [prize]! Every 5 bets = 1 FREE ticket. Results on Discord!");
const minutes = ref(10);

const syncFromState = () => {
  if (props.state.hypeShoutPhrase) phrase.value = props.state.hypeShoutPhrase;
  if (props.state.hypeShoutMinutes) minutes.value = props.state.hypeShoutMinutes;
};

watch(() => props.state.hypeShoutPhrase, syncFromState);
watch(() => props.state.hypeShoutMinutes, syncFromState);
onMounted(syncFromState);

const call = async (name, ...args) => {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return await window.go.main.App[name](...args);
  }
};

const saveConfig = async () => {
  await call('SetHypeShoutConfig', phrase.value, parseInt(minutes.value));
  emit('refresh');
};

const toggleHype = async () => {
  await call('ToggleHypeShout', !props.state.hypeShoutEnabled);
  emit('refresh');
};
</script>

<template>
  <div class="card">
    <div class="row" style="justify-content: space-between; margin-bottom: 8px;">
      <h3 style="margin:0">Raffle Hype Shout (Timer)</h3>
      <button :class="state.hypeShoutEnabled ? 'stop' : 'ok-btn'" @click="toggleHype">
        {{ state.hypeShoutEnabled ? 'Disable Hype' : 'Enable Hype' }}
      </button>
    </div>
    <p class="sub">Sends a periodic shout to the room. Use <code>[prize]</code> to auto-insert the raffle prize name.</p>
    
    <div class="row" style="margin-top: 12px; align-items: flex-end;">
      <div style="flex: 1; display:flex; flex-direction:column; gap:4px;">
        <label style="font-size:12px; color:var(--muted)">Shout Message</label>
        <input type="text" v-model="phrase" placeholder="e.g. Win a [prize]! Check Discord for details!" style="width:100%">
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
</style>
