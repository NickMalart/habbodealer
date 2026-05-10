<script setup>
const props = defineProps({
  snapshot: String
});
const emit = defineEmits(['refresh']);

async function call(name, ...args) {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return window.go.main.App[name](...args);
  }
  console.error(`Backend method ${name} not available.`);
  throw new Error(`Backend method ${name} is not available.`);
}

async function copyDebugToClipboard() {
  if (!props.snapshot) return;
  try {
    await navigator.clipboard.writeText(props.snapshot);
    alert('Debug info copied to clipboard.');
  } catch (err) {
    alert('Failed to copy to clipboard.');
  }
}

async function clearDebug() {
  await call('ClearDebugSnapshot');
  emit('refresh');
}
</script>

<template>
  <div class="card">
    <div class="row" style="justify-content: space-between; margin-bottom: 10px;">
      <h2 style="margin: 0;">Debug</h2>
      <div class="row">
        <button class="alt" @click="$emit('refresh')">Refresh Debug</button>
        <button class="alt" @click="copyDebugToClipboard">Copy Debug</button>
        <button class="stop" @click="clearDebug">Clear Debug</button>
      </div>
    </div>
    <textarea :value="snapshot" placeholder="Debug output appears here..." readonly></textarea>
    <p class="sub" style="margin-top: 8px;">Use Copy Debug and paste it into chat when troubleshooting.</p>
  </div>
</template>
