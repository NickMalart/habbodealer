<script setup>
import { ref, watch, onMounted } from 'vue';
import NewRaffleModal from './NewRaffleModal.vue';

const props = defineProps({
  state: Object
});

const emit = defineEmits(['refresh']);

const bonusEvery = ref(5);
const showNewRaffleModal = ref(false);

const updateBonus = () => {
  if (props.state.bonusEvery) {
    bonusEvery.value = props.state.bonusEvery;
  }
};

watch(() => props.state.bonusEvery, updateBonus);

onMounted(() => {
  updateBonus();
});

const call = async (name, ...args) => {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App[name]) {
    return await window.go.main.App[name](...args);
  }
};

const toggleEnabled = async () => {
  await call('SetEnabled', !props.state.enabled);
  emit('refresh');
};

const saveBonusRule = async () => {
  await call('SetBonusEvery', bonusEvery.value > 0 ? bonusEvery.value : 5);
  emit('refresh');
};

const toggleTicketAnnounce = async (e) => {
  await call('SetTicketAnnounceEnabled', e.target.checked);
};

const toggleTicketProgress = async (e) => {
  await call('SetTicketProgressEnabled', e.target.checked);
};

const toggleSession = async () => {
  if (props.state.currentSession) {
    await call('StopRaffle');
    emit('refresh');
  } else {
    showNewRaffleModal.value = true;
  }
};

const createRaffle = async (data) => {
  try {
    await call('CreateRaffle', data.name, data.prize, data.qty, data.heroDataUrl, data.heroFileName, data.startAt, data.endAt, data.sponsorEnabled, data.sponsorName, data.sponsorRoom, data.sponsorDataUrl, data.sponsorFileName);
    showNewRaffleModal.value = false;
    emit('refresh');
  } catch (err) {
    alert(`Could not create raffle: ${err}`);
  }
};

const refresh = () => emit('refresh');
</script>

<template>
  <div>
    <div class="card">
      <div class="row" style="justify-content: space-between;">
        <div class="row">
          <label for="bonus">Bonus every</label>
          <input id="bonus" type="number" min="1" v-model="bonusEvery">
          <button class="alt" @click="saveBonusRule">Save Bonus Rule</button>
        </div>
      </div>
      <div class="row" style="margin-top: 12px;">
        <button @click="toggleEnabled">{{ state.enabled ? 'Disable' : 'Enable' }}</button>
        <button 
          :class="state.currentSession ? 'stop' : 'alt'" 
          @click="toggleSession"
        >
          {{ state.currentSession ? 'Stop Session' : 'Start New Raffle...' }}
        </button>
        <button class="alt" @click="refresh">Refresh</button>
        <label style="display:flex;align-items:center;gap:6px;cursor:pointer;">
          <input type="checkbox" :checked="state.ticketAnnounceEnabled" @change="toggleTicketAnnounce"> Announce tickets
        </label>
        <label style="display:flex;align-items:center;gap:6px;cursor:pointer;">
          <input type="checkbox" :checked="state.ticketProgressEnabled" @change="toggleTicketProgress"> Shout ticket progress
        </label>
      </div>
    </div>
    <NewRaffleModal 
      v-if="showNewRaffleModal" 
      @close="showNewRaffleModal = false"
      @create-raffle="createRaffle"
    />
  </div>
</template>
