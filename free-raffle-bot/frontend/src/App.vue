<script setup>
import { ref, onMounted, onUnmounted } from 'vue';
import StatusHeader from './components/StatusHeader.vue';
import ControlPanel from './components/ControlPanel.vue';
import HypeShout from './components/HypeShout.vue';
import JoinShout from './components/JoinShout.vue';
import AutoMsg from './components/AutoMsg.vue';
import DiscordConfig from './components/DiscordConfig.vue';
import ActiveSession from './components/ActiveSession.vue';
import SessionHistory from './components/SessionHistory.vue';
import DebugPanel from './components/DebugPanel.vue';
import CompletedSessions from './components/CompletedSessions.vue';

const state = ref({
  connected: false,
  inRoom: false,
  enabled: false,
  bonusEvery: 5,
  ticketAnnounceEnabled: false,
  ticketProgressEnabled: false,
  raffleName: '',
  rafflePrizeName: '',
  rafflePrizeQty: 1,
  raffleAutoUpdate: true,
  sponsorEnabled: false,
  sponsorName: '',
  sponsorRoomName: '',
  raffleHeroImageName: '',
  joinShoutEnabled: false,
  joinShoutPhrase: '',
  hypeShoutEnabled: false,
  hypeShoutPhrase: '',
  hypeShoutMinutes: 10,
  nextHypeShoutAt: '',
  autoMsgEnabled: false,
  autoMsgPhrase: '',
  autoMsgMinutes: 10,
  nextAutoMsgAt: '',
  currentSession: null,
  sessions: []
});

const debugSnapshot = ref('');
const activeTab = ref('main');

const refresh = async () => {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App.GetState) {
    const s = await window.go.main.App.GetState();
    if (s) state.value = s;
  }
};

const refreshDebug = async () => {
  if (window.go && window.go.main && window.go.main.App && window.go.main.App.GetDebugSnapshot) {
    const txt = await window.go.main.App.GetDebugSnapshot();
    if (typeof txt === 'string') debugSnapshot.value = txt;
  }
};

let refreshInterval, debugInterval;

onMounted(() => {
  refresh();
  refreshDebug();
  refreshInterval = setInterval(refresh, 8000);
  debugInterval = setInterval(refreshDebug, 10000);

  if (window.runtime && window.runtime.EventsOn) {
    window.runtime.EventsOn('raffleStateUpdate', (s) => {
      if (s) state.value = s;
    });
    window.runtime.EventsOn('raffleDebugUpdate', (snapshot) => {
      if (typeof snapshot === 'string') debugSnapshot.value = snapshot;
    });
  }
});

onUnmounted(() => {
  clearInterval(refreshInterval);
  clearInterval(debugInterval);
});
</script>

<template>
  <div class="wrap">
    <div class="card">
      <h1 class="title">Free Raffle Bot</h1>
      <p class="sub">Adds 1 ticket on first bet, then +1 ticket every Nth bet.</p>
    </div>

    <div class="card">
      <div class="tabs">
        <button 
          @click="activeTab = 'main'" 
          class="tab-btn" 
          :class="{ active: activeTab === 'main' }"
        >Main</button>
        <button 
          @click="activeTab = 'debug'" 
          class="tab-btn" 
          :class="{ active: activeTab === 'debug' }"
        >Debug</button>
        <button
          @click="activeTab = 'completed'"
          class="tab-btn"
          :class="{ active: activeTab === 'completed' }"
        >Completed</button>
      </div>
    </div>

    <div v-show="activeTab === 'main'">
      <HypeShout :state="state" @refresh="refresh" />
      <JoinShout :state="state" @refresh="refresh" />
      <AutoMsg :state="state" @refresh="refresh" />
      <StatusHeader :state="state" @refresh="refresh" />
      <ControlPanel :state="state" @refresh="refresh" />
      <DiscordConfig :state="state" @refresh="refresh" />
      <ActiveSession :state="state" @refresh="refresh" />
      <SessionHistory :state="state" @refresh="refresh" />
    </div>

    <div v-show="activeTab === 'completed'">
      <CompletedSessions :state="state" @refresh="refresh" />
    </div>

    <div v-show="activeTab === 'debug'">
      <DebugPanel :snapshot="debugSnapshot" @refresh="refreshDebug" />
    </div>
  </div>
</template>

<style>
:root {
  --bg: #0d1c24;
  --panel: #132b36;
  --panel2: #10303f;
  --text: #e8f2f4;
  --muted: #b4c8ce;
  --accent: #ffc857;
  --ok: #62c370;
  --warn: #ff6b6b;
}

* { box-sizing: border-box; }

body {
  margin: 0;
  color: var(--text);
  font-family: "Segoe UI", "Trebuchet MS", sans-serif;
  background: radial-gradient(circle at 15% 20%, #1c4457 0%, var(--bg) 40%),
              radial-gradient(circle at 85% 75%, #2c1f34 0%, rgba(44,31,52,0) 36%);
  min-height: 100vh;
}

.wrap {
  max-width: 980px;
  margin: 0 auto;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.card {
  background: linear-gradient(130deg, rgba(255,255,255,0.03), rgba(255,255,255,0.01)), var(--panel);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: 14px;
  padding: 14px;
  margin-bottom: 14px;
}

.row { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.title { font-size: 24px; margin: 0 0 4px; }
.sub { margin: 0; color: var(--muted); }

.status {
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 12px;
  background: var(--panel2);
  border: 1px solid rgba(255,255,255,0.14);
}

button {
  border: 0;
  border-radius: 10px;
  padding: 10px 12px;
  font-weight: 700;
  cursor: pointer;
  background: var(--accent);
  color: #1f1f1f;
}

button.alt { background: #8ab6c9; }
button.stop { background: var(--warn); color: #fff; }
button:disabled { opacity: 0.5; cursor: not-allowed; }

input[type="number"],
input[type="text"],
input[type="url"],
input[type="datetime-local"] {
  border: 1px solid rgba(255,255,255,0.25);
  border-radius: 8px;
  padding: 8px;
  background: #0b1b22;
  color: var(--text);
}

input[type="number"] { width: 90px; }
input[type="text"], input[type="url"] { min-width: 260px; }

table {
  width: 100%;
  border-collapse: collapse;
  font-size: 14px;
}

th, td {
  text-align: left;
  padding: 8px;
  border-bottom: 1px solid rgba(255,255,255,0.1);
}

th { color: var(--muted); font-weight: 600; }

.muted { color: var(--muted); }

.tabs { display: flex; gap: 8px; }

.tab-btn {
  background: #355a68;
  color: #eaf3f6;
  border: 1px solid rgba(255,255,255,0.2);
}

.tab-btn.active {
  background: var(--accent);
  color: #1f1f1f;
  border-color: transparent;
}

.hidden { display: none; }

textarea {
  width: 100%;
  min-height: 260px;
  resize: vertical;
  border-radius: 10px;
  border: 1px solid rgba(255,255,255,0.2);
  background: #0b1b22;
  color: var(--text);
  padding: 10px;
  font-family: Consolas, "Courier New", monospace;
  font-size: 12px;
  line-height: 1.4;
}

.raffle-hero-preview {
  width: 220px;
  max-width: 100%;
  border: 1px solid rgba(255,255,255,0.2);
  border-radius: 10px;
  display: block;
}

.winner-note {
  padding: 8px 10px;
  border-radius: 8px;
  background: rgba(98,195,112,0.12);
  border: 1px solid rgba(98,195,112,0.4);
  color: #c9f5ce;
  font-size: 13px;
}
</style>
