<script setup>
const props = defineProps({
  state: Object
});
</script>

<template>
  <div class="card">
    <div class="row">
      <span class="status">{{ state.connected ? 'Connected' : 'Disconnected' }}</span>
      <span class="status">{{ state.inRoom ? 'In room' : 'Out of room' }}</span>
      <span class="status">{{ state.enabled ? 'Enabled' : 'Disabled' }}</span>
    </div>
  </div>
</template>
