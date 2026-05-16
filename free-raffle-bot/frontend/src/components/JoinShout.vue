<script setup>
import { ref, watch, onMounted } from 'vue';

const props = defineProps({
  state: Object
});

const emit = defineEmits(['refresh']);

const phrase = ref("Welcome {name}!, Win [prize]! 1st bet = 1 Ticket + Every 5th = FREE Ticket! See Discord!");
const delay = ref(2);
const cooldown = ref(5);

const syncFromState = () => {
  if (props.state.joinShoutPhrase) phrase.value = props.state.joinShoutPhrase;
  if (props.state.joinShoutDelay !== undefined) delay.value = props.state.joinShoutDelay;
  if (props.state.joinShoutCooldownMinutes !== undefined) cooldown.value = props.state.joinShoutCooldownMinutes;
};

watch(() => props.state.joinShoutPhrase, () => {
  phrase.value = props.state.joinShoutPhrase;
});

watch(() => props.state.joinShoutDelay, () => {
  delay.value = props.state.joinShoutDelay;
});

watch(() => props.state.joinShoutCooldownMinutes, () => {
  cooldown.value = props.state.joinShoutCooldownMinutes;
});

onMounted(() => {
  syncFromState();
});

const call = async (name, ...args) => {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return await window.go.main.App[name](...args);
  }
};

const saveConfig = async () => {
  await call('SetJoinShoutConfig', props.state.joinShoutEnabled, phrase.value);
  await call('SetJoinShoutDelay', parseInt(delay.value) || 0);
  await call('SetJoinShoutCooldown', parseInt(cooldown.value) || 0);
  emit('refresh');
};

const toggleJoin = async () => {
  await call('ToggleJoinShout', !props.state.joinShoutEnabled);
  emit('refresh');
};
</script>

<template>
  <div class="card">
    <div class="row" style="justify-content: space-between; margin-bottom: 8px;">
      <div style="display:flex; flex-direction:column; gap:2px;">
        <h3 style="margin:0">Join Shout (Real-time)</h3>
        <span v-if="state.joinShoutEnabled" class="status-active">Active - Watching for joiners</span>
      </div>
      <button :class="state.joinShoutEnabled ? 'stop' : 'ok-btn'" @click="toggleJoin">
        {{ state.joinShoutEnabled ? 'Disable Join Shout' : 'Enable Join Shout' }}
      </button>
    </div>
    <p class="sub">Shouts when someone joins the room. Use <code>{name}</code> and <code>{prize}</code> placeholders.</p>
    
    <div class="row" style="margin-top: 12px; align-items: flex-end; gap: 8px;">
      <div style="flex: 3; display:flex; flex-direction:column; gap:4px;">
        <label style="font-size:12px; color:var(--muted)">Shout Message</label>
        <input type="text" v-model="phrase" placeholder="e.g. Hey {name}, Congrats! Entered for {prize}!" style="width:100%">
      </div>
      <div style="flex: 1; display:flex; flex-direction:column; gap:4px;">
        <label style="font-size:12px; color:var(--muted)">Delay (sec)</label>
        <input type="number" v-model="delay" min="0" max="60" style="width:100%">
      </div>
      <div style="flex: 1; display:flex; flex-direction:column; gap:4px;">
        <label style="font-size:12px; color:var(--muted)">Cooldown (min)</label>
        <input type="number" v-model="cooldown" min="0" max="1440" style="width:100%">
      </div>
      <button class="alt" @click="saveConfig">Save Config</button>
    </div>
  </div>
</template>

<style scoped>
.ok-btn { background: var(--ok); color: #fff; }
.status-active {
  font-size: 12px;
  color: #62c370;
  font-weight: bold;
}
</style>
