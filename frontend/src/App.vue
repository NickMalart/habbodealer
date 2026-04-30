<template>
  <div class="poker-config-section">

    <!-- Tab bar -->
      <div class="tab-bar">
      <button
        v-for="tab in ['Home', 'Trade', 'Game History', 'Stats', 'Logs', 'Raffle', 'Utility']"
        :key="tab"
        :class="['tab-btn', { active: activeTab === tab }]"
        @click="activeTab = tab"
      >{{ tab }}</button>
      </div>

    <div v-if="activeTab === 'Home'">
      <h2 class="section-title">Home</h2>
      <p class="config-intro">Home Page!</p>
      <div class="game-card-grid">
        <button v-for="game in gameGuides" :key="game.key" class="game-card" @click="openGameGuide(game)">
          <div class="game-card-title">{{ game.title }}</div>
          <span class="game-card-summary">{{ game.summary }}</span>
          <span class="game-card-action">Click for details</span>
        </button>
      </div>

      <div class="casino-panel">
        <div class="casino-panel-inner">
          <div class="casino-info">
            <div class="casino-status-label">Casino Status</div>
            <div :class="['casino-status', casinoStatusKey]">{{ casinoStatus }}</div>
          </div>
          <div class="casino-actions">
            <button v-if="casinoStatusKey === 'stopped'" type="button" class="copy-btn start-casino-btn" @click="startCasino">Start Casino</button>
            <button v-if="casinoStatusKey !== 'stopped'" type="button" class="copy-btn history-danger-btn" @click="stopCasino">Stop</button>
          </div>
        </div>
        <div v-if="casinoStatusKey !== 'stopped'" class="trade-limits-home">Trade limits: max {{ maxUniqueItemsInput }} unique items, max {{ maxQuantityPerItemInput }} per item</div>
      </div>

    </div>

      <div class="dice-setup-modal-backdrop" v-if="showDiceSetupModal" @click="showDiceSetupModal = false">
        <div class="dice-setup-modal" @click.stop>
          <div class="game-guide-header">
            <h3 class="section-title game-guide-title">Roll all {{ (onlyUnderOverMode || underOver7Mode) ? 2 : 5 }} dice</h3>
            <button type="button" class="copy-btn" @click="showDiceSetupModal = false">Close</button>
          </div>
          <p class="game-guide-text">Please roll all {{ (onlyUnderOverMode || underOver7Mode) ? 2 : 5 }} dice in the game. Circles will turn green as each dice is recorded.</p>
          <div class="dice-circles">
            <div v-for="(slot, idx) in diceSetup" :key="idx" :class="['dice-circle', { rolled: diceSetup[idx] && diceSetup[idx].rolled }]">{{ idx + 1 }}</div>
          </div>
          <div class="game-guide-block">
            <div class="game-guide-label">Status</div>
            <div class="game-guide-text">{{ (diceSetup.filter(d => d.rolled).length) || 0 }} / {{ diceSetup.length || ((onlyUnderOverMode || underOver7Mode) ? 2 : 5) }} rolled</div>
          </div>
        </div>
      </div>

        <div class="dealer-name-modal-backdrop" v-if="showDealerNameModal" @click="showDealerNameModal = false">
          <div class="game-guide-modal dealer-name-modal" @click.stop>
            <div class="game-guide-header">
              <h3 class="section-title game-guide-title">Enter your Habbo name</h3>
              <button type="button" class="copy-btn" @click="cancelDealerName">Close</button>
            </div>
            <p class="game-guide-text">This name will be shown on the live dashboard as the dealer. Please enter your Habbo username.</p>
            <div class="game-guide-block">
              <input v-model="dealerNameInput" type="text" placeholder="Your Habbo username" autocapitalize="off" autocorrect="off" spellcheck="false" style="width:100%;padding:8px;border-radius:4px;border:1px solid #ccc;max-width:420px;" />
            </div>
            <div class="game-guide-block">
              <div class="game-guide-label">Room Name</div>
              </div>
            };
          },
          computed: {
            filteredArchivedList() {
              if (!this.archivedList || this.archivedList.length === 0) return [];
              const q = (this.archivedSearch || '').trim().toLowerCase();
              if (!q) return this.archivedList;
              return this.archivedList.filter(e => {
                const name = (e.name || '').toLowerCase();
                const start = (e.startAt || '').toLowerCase();
                const end = (e.endAt || '').toLowerCase();
                return name.includes(q) || start.includes(q) || end.includes(q) || String(e.index).includes(q);
              });
            },
            <div style="font-size:12px;color:#bdbdbd;margin-top:8px;">
              Max Unique Items = how many different item types the player may offer. Max Quantity Per Item = max allowed amount for any one item type.
            </div>
            <div class="game-guide-block" style="margin-top:8px;">
              <div class="game-guide-label">Dealer Mode</div>
              <label style="font-size:13px;display:flex;align-items:center;gap:8px;">
                <input type="checkbox" v-model="onlyUnderOverMode" />
                Only Under/Over 7 (only show Over/Under to players)
              </label>
              <label style="font-size:13px;display:flex;align-items:center;gap:8px;margin-top:6px;">
                <input type="checkbox" v-model="underOver7Mode" />
                Enable Under/Over-7 mode (allow '7' x{{ uo7Multiplier }} payout)
                <select v-model.number="uo7Multiplier" :disabled="!underOver7Mode" style="margin-left:8px;">
                  <option v-for="n in [2,3,4,5]" :key="n" :value="n">x{{ n }}</option>
                </select>
              </label>
              <label style="font-size:13px;display:flex;align-items:center;gap:8px;margin-top:6px;">
                <input type="checkbox" v-model="riskModeEnabledInput" :disabled="underOver7Mode" />
                Enable Risk Mode
              </label>
            </div>
            <div style="display:flex;justify-content:flex-end;gap:8px;margin-top:12px;">
              <button class="copy-btn" @click="cancelDealerName">Cancel</button>
              <button class="copy-btn" @click="confirmDealerName">Start</button>
            </div>
          </div>
        </div>

        <div class="game-guide-modal-backdrop" v-if="activeGameGuide" @click="closeGameGuide">
        <div class="game-guide-modal" @click.stop>
          <div class="game-guide-header">
            <h3 class="section-title game-guide-title">{{ activeGameGuide.title }}</h3>
            <button type="button" class="copy-btn" @click="closeGameGuide">Close</button>
          </div>
          <p class="game-guide-text">{{ activeGameGuide.description }}</p>
          <div class="game-guide-block">
            <div class="game-guide-label">How it works</div>
            <div class="game-guide-text">{{ activeGameGuide.howItWorks }}</div>
          </div>
          <div class="game-guide-block">
            <div class="game-guide-label">What the player does</div>
            <div class="game-guide-text">{{ activeGameGuide.playerFlow }}</div>
          </div>
          <div class="game-guide-block">
            <div class="game-guide-label">What the dealer does</div>
            <div class="game-guide-text">{{ activeGameGuide.dealerFlow }}</div>
          </div>
        </div>
      </div>

    <div v-if="activeTab === 'Utility'">
      <h2 class="section-title">Auto Shout</h2>
      <p class="config-intro">Configure an automatic periodic shout message.</p>

      <div class="auto-shout-panel">
        <div class="form-group">
          <label>Phrase</label>
          <input type="text" v-model="autoShoutPhrase" placeholder="Enter phrase to shout" />
        </div>

        <div class="preset-row" style="margin-bottom:8px;">
          <div class="game-guide-label" style="display:inline-block;margin-right:8px;margin-bottom:4px;">Presets</div>
          <div style="display:inline-flex;gap:8px;flex-wrap:wrap;align-items:center;">
            <button type="button" class="copy-btn preset-btn" @click="setAutoShoutPreset('See My Hand - rollorigins.club')">See My Hand - rollorigins.club</button>
            <button type="button" class="copy-btn" @click="addAutoShoutPreset" title="Save current phrase as preset">Add Preset</button>
          </div>
          <div style="margin-top:8px;display:flex;gap:8px;flex-wrap:wrap;">
            <template v-for="(p, idx) in autoShoutPresets" :key="'preset-'+idx">
              <div style="display:inline-flex;align-items:center;gap:6px;">
                <button type="button" class="copy-btn preset-btn" @click="setAutoShoutPreset(p)">{{ p }}</button>
                <button type="button" class="copy-btn delete-preset" @click="deleteAutoShoutPreset(idx)" title="Delete preset">×</button>
              </div>
            </template>
          </div>
        </div>

        <div class="form-group">
          <label>Seconds</label>
          <input type="number" min="1" v-model.number="autoShoutSeconds" />
        </div>

        <div style="display:flex;gap:8px;justify-content:center;align-items:center;">
          <button class="save-button" @click="saveAutoShout">Save</button>
          <button class="save-button" @click="toggleAutoShout">{{ autoShoutEnabled ? 'Stop Auto Shout' : 'Start Auto Shout' }}</button>
        </div>

        <!-- Auto Shout slot #2 (duplicate) -->
        <div class="auto-shout-panel" style="margin-top:12px;padding:10px;border-top:1px dashed rgba(255,255,255,0.04);">
          <div class="form-group">
            <label>Phrase</label>
            <input type="text" v-model="autoShoutPhrase2" placeholder="Enter phrase to shout" />
          </div>

          <div class="preset-row" style="margin-bottom:8px;">
            <div class="game-guide-label" style="display:inline-block;margin-right:8px;margin-bottom:4px;">Presets</div>
            <div style="display:inline-flex;gap:8px;flex-wrap:wrap;align-items:center;">
              <button type="button" class="copy-btn preset-btn" @click="setAutoShoutPreset2('See My Hand - rollorigins.club')">See My Hand - rollorigins.club</button>
              <button type="button" class="copy-btn" @click="addAutoShoutPreset2" title="Save current phrase as preset">Add Preset</button>
            </div>
            <div style="margin-top:8px;display:flex;gap:8px;flex-wrap:wrap;">
              <template v-for="(p, idx) in autoShoutPresets2" :key="'preset2-'+idx">
                <div style="display:inline-flex;align-items:center;gap:6px;">
                  <button type="button" class="copy-btn preset-btn" @click="setAutoShoutPreset2(p)">{{ p }}</button>
                  <button type="button" class="copy-btn delete-preset" @click="deleteAutoShoutPreset2(idx)" title="Delete preset">×</button>
                </div>
              </template>
            </div>
          </div>

          <div class="form-group">
            <label>Seconds</label>
            <input type="number" min="1" v-model.number="autoShoutSeconds2" />
          </div>

          <div style="display:flex;gap:8px;justify-content:center;align-items:center;">
            <button class="save-button" @click="saveAutoShout2">Save</button>
            <button class="save-button" @click="toggleAutoShout2">{{ autoShoutEnabled2 ? 'Stop Auto Shout' : 'Start Auto Shout' }}</button>
          </div>
        </div>

        <div style="margin-top:12px;border-top:1px dashed rgba(255,255,255,0.04);padding-top:12px;">
          <h3 class="section-subtitle">Raffles</h3>
          <div style="display:flex;gap:8px;align-items:center;margin-bottom:8px;">
            <button class="copy-btn" @click="openArchivedModal">Show Existing Raffle</button>
          </div>
        </div>

        <div style="margin-top:18px;border-top:1px solid rgba(255,255,255,0.04);padding-top:12px;">
          <h3 class="section-subtitle">Dealer Open</h3>
          <p class="config-intro">Control automatic "Dealer open" announcements and trade-window timeout.</p>
          <div class="form-group">
            <label style="display:flex;align-items:center;gap:8px;"><input type="checkbox" v-model="dealerOpenEnabled" /> Enable Dealer Open announcement</label>
          </div>
          <div class="form-group">
            <label>Trade window timeout (seconds)</label>
            <input type="number" min="1" v-model.number="dealerTradeSeconds" />
          </div>
          <div class="form-group">
            <label>Dealer announce interval (seconds)</label>
            <input type="number" min="1" v-model.number="dealerAnnounceSeconds" />
          </div>
          <div style="display:flex;gap:8px;justify-content:center;align-items:center;">
            <button class="save-button" @click="saveDealerOpen">Save</button>
          </div>
        </div>
        <div class="form-group" style="margin-top:12px;">
          <fieldset style="border:1px solid rgba(255,255,255,0.08);padding:10px;border-radius:6px;">
            <legend style="font-weight:600;padding:0 6px;">Block Packets</legend>
            <div style="color:#bdbdbd;font-size:12px;margin-bottom:8px;">
              Select packet types to block (split by direction)
            </div>

            <div style="display:flex;gap:12px;flex-wrap:wrap;">
              <div style="flex:1;min-width:200px;">
                <div class="game-guide-label" style="margin-bottom:6px;">Incoming</div>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;">
                  <input type="checkbox" v-model="blockRecommendedRooms" @change="toggleBlockRecommendedRooms" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Recommended rooms (header 351)</span>
                    <span class="packet-info" tabindex="0" aria-label="Recommended rooms info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Recommended rooms — header 351</div><div class="tooltip-body">Server-sent list of recommended rooms and metadata.</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockFavouriteRoomResults" @change="toggleBlockFavouriteRoomResults" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Favourite room results (header 61)</span>
                    <span class="packet-info" tabindex="0" aria-label="Favourite room results info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Favourite room results — header 61</div><div class="tooltip-body">Incoming list of favourite rooms for the user or room.</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockSlideObjectBundle" @change="toggleBlockSlideObjectBundle" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Slide object bundle (header 230)</span>
                    <span class="packet-info" tabindex="0" aria-label="Slide object bundle info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Slide object bundle — header 230</div><div class="tooltip-body">Bundles object slide/movement updates (furniture/object positions). May affect room object syncing and visuals.</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockStatusEffects" @change="toggleBlockStatusEffects" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Status effects (header 1242)</span>
                    <span class="packet-info" tabindex="0" aria-label="Status effects info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Status effects — header 1242</div><div class="tooltip-body">Carries status/effect updates (timers, mode changes, game-effect notifications).</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockRemoveBuddy" @change="toggleBlockRemoveBuddy" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Remove buddy (header 138)</span>
                    <span class="packet-info" tabindex="0" aria-label="Remove buddy info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Remove buddy — header 138</div><div class="tooltip-body">Notifies the client that a friend was removed.</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockFriendListUpdate" @change="toggleBlockFriendListUpdate" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Friend list update (header 13)</span>
                    <span class="packet-info" tabindex="0" aria-label="Friend list update info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Friend list update — header 13</div><div class="tooltip-body">Contains friend-list changes (online status, adds/removes).</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockArticlesPage" @change="toggleBlockArticlesPage" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Articles page (header 681)</span>
                    <span class="packet-info" tabindex="0" aria-label="Articles page info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Articles page — header 681</div><div class="tooltip-body">Incoming page containing article titles and snippets (news/articles listing).</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockCalendarEvents" @change="toggleBlockCalendarEvents" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Calendar events (header 683)</span>
                    <span class="packet-info" tabindex="0" aria-label="Calendar events info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Calendar events — header 683</div><div class="tooltip-body">Incoming list of scheduled events, times, and brief descriptions.</div></span>
                    </span>
                  </span>
                </label>
                
                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockUserBanned" @change="toggleBlockUserBanned" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>User banned (header 35)</span>
                    <span class="packet-info" tabindex="0" aria-label="User banned info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">User banned — header 35</div><div class="tooltip-body">Server notification that a user was banned or restricted. Blocking may suppress ban notices.</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockIncoming4095" @change="toggleBlockIncoming4095" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Raw 4095 packets (header 4095)</span>
                    <span class="packet-info" tabindex="0" aria-label="Raw 4095 info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Raw 4095 — header 4095</div><div class="tooltip-body">Low-level/unknown raw packets such as 0x7f7f 'RB'. Block if you want to suppress them.</div></span>
                    </span>
                  </span>
                </label>
              </div>

              <div style="flex:1;min-width:200px;">
                <div class="game-guide-label" style="margin-bottom:6px;">Outgoing</div>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;">
                  <input type="checkbox" v-model="blockRecommendedRoomsOutgoing" @change="toggleBlockRecommendedRoomsOutgoing" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Get recommended rooms (header 264)</span>
                    <span class="packet-info" tabindex="0" aria-label="Get recommended rooms outgoing info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Get recommended rooms (outgoing) — header 264</div><div class="tooltip-body">Outgoing request to fetch recommended rooms from the server.</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockSlideObjectBundleOutgoing" @change="toggleBlockSlideObjectBundleOutgoing" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Slide object bundle (header 230)</span>
                    <span class="packet-info" tabindex="0" aria-label="Slide object bundle outgoing info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Slide object bundle (outgoing) — header 230</div><div class="tooltip-body">Sends batched object movement/slide updates to the server.</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockPollEventEligibilityOutgoing" @change="toggleBlockPollEventEligibilityOutgoing" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Poll event eligibility (header 1120)</span>
                    <span class="packet-info" tabindex="0" aria-label="Poll event eligibility outgoing info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Poll event eligibility (outgoing) — header 1120</div><div class="tooltip-body">Outgoing request to check whether the user is eligible for polls/events.</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockGetPageArticlesOutgoing" @change="toggleBlockGetPageArticlesOutgoing" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Get page articles (header 680)</span>
                    <span class="packet-info" tabindex="0" aria-label="Get page articles outgoing info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Get page articles (outgoing) — header 680</div><div class="tooltip-body">Outgoing request to fetch page articles/listings from the server.</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockGetCalendarEventsOutgoing" @change="toggleBlockGetCalendarEventsOutgoing" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Get calendar events (header 682)</span>
                    <span class="packet-info" tabindex="0" aria-label="Get calendar events outgoing info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Get calendar events (outgoing) — header 682</div><div class="tooltip-body">Outgoing request to fetch calendar events from the server.</div></span>
                    </span>
                  </span>
                </label>

                <label style="display:flex;align-items:center;gap:8px;color:#e0e0e0;margin-top:6px;">
                  <input type="checkbox" v-model="blockFriendListUpdateOutgoing" @change="toggleBlockFriendListUpdateOutgoing" />
                  <span style="display:inline-flex;align-items:center;gap:6px;">
                    <span>Friend list update (header 15)</span>
                    <span class="packet-info" tabindex="0" aria-label="Friend list update outgoing info" @mouseenter="showPacketTooltip($event)" @mouseleave="hidePacketTooltip" @focus="showPacketTooltip($event)" @blur="hidePacketTooltip">ℹ
                      <span class="tooltip"><div class="tooltip-header">Friend list update (outgoing) — header 15</div><div class="tooltip-body">Outgoing friend-list sync/update request.</div></span>
                    </span>
                  </span>
                </label>
              </div>
            </div>
          </fieldset>
        </div>
      </div>
    </div>

    <!-- Trade tab -->
    <div v-if="activeTab === 'Trade'">

      <h2 class="section-title">Double Payout Check</h2>
      <p class="trade-hint" v-if="activeBetSourceLabel">
        Showing {{ activeBetSourceLabel }} data until the round is fully complete.
      </p>
      <div class="trade-empty" v-if="activeBetItems.length === 0">
        No live or saved round bet data yet.
      </div>
      <table v-else class="catalog-table">
        <thead><tr><th>Item</th><th>Bet</th><th>Need Stock</th><th>Payout</th><th>Have</th><th>Status</th></tr></thead>
        <tbody>
          <tr v-for="(row, index) in payoutRows" :key="`payout-${index}`">
            <td><span class="catalog-label">{{ row.displayName }}</span></td>
            <td><span class="catalog-label">{{ row.betQty }}</span></td>
            <td><span class="catalog-label">{{ row.required }}</span></td>
            <td><span class="catalog-label">{{ row.payoutTotal }}</span></td>
            <td><span class="catalog-label">{{ row.have }}</span></td>
            <td><span class="catalog-label" :class="{ 'unlisted-value': row.short > 0 }">{{ row.short > 0 ? `Short ${row.short}` : 'OK' }}</span></td>
          </tr>
        </tbody>
      </table>
      <p class="trade-hint" v-if="activeBetItems.length > 0 && !canCoverPayout">
        You do not have enough stock to return double payout (bet + match).
      </p>
      <p class="trade-hint" v-if="activeBetItems.length > 0 && canCoverPayout">
        Your hand can return double payout (bet + match).
      </p>

      <hr class="trade-divider" />

      <!-- Your Hand -->
      <h2 class="section-title">Your Hand</h2>
      <div v-if="handItems.length === 0" class="trade-empty">
        No hand data yet.
        <span style="font-size:12px;color:#666;">Refreshes every 30s and when a trade opens.</span>
      </div>
      <table v-else class="catalog-table">
        <thead><tr><th>Item</th><th>Qty</th></tr></thead>
        <tbody>
          <tr v-for="(item, index) in handItems" :key="`hand-${index}`">
            <td><span class="catalog-label">{{ item.displayName }}</span></td>
            <td><span class="catalog-label">{{ item.Quantity }}</span></td>
          </tr>
        </tbody>
      </table>
      <hr class="trade-divider" />

      <!-- Player's Offer -->
      <div v-if="casinoStatusKey !== 'stopped'" class="trade-hint">Trade limits: max {{ maxUniqueItemsInput }} unique items, max {{ maxQuantityPerItemInput }} per item</div>
      <h2 class="section-title">Player Offer</h2>
      <div v-if="tradeItems.length === 0" class="trade-empty">
        No active trade items detected.<br />
        <span style="font-size:12px;color:#666">Items appear here when the partner places furniture in the trade.</span>
      </div>
      <table v-else class="catalog-table">
        <thead><tr><th>Item</th><th>Qty</th></tr></thead>
        <tbody>
          <tr v-for="(item, index) in tradeItems" :key="`trade-${index}`">
            <td><span class="catalog-label">{{ item.displayName }}</span></td>
            <td><span class="catalog-label">{{ item.Quantity }}</span></td>
          </tr>
        </tbody>
      </table>

      <hr class="trade-divider" />

      <!-- Your Offer -->
      <h2 class="section-title">Your Offer To Them</h2>
      <div v-if="ownTradeItems.length === 0" class="trade-empty">
        Nothing added by you yet.
      </div>
      <table v-else class="catalog-table">
        <thead><tr><th>Item</th><th>Qty</th></tr></thead>
        <tbody>
          <tr v-for="(item, index) in ownTradeItems" :key="`own-trade-${index}`">
            <td><span class="catalog-label">{{ item.displayName }}</span></td>
            <td><span class="catalog-label">{{ item.Quantity }}</span></td>
          </tr>
        </tbody>
      </table>

    </div>

    <div v-if="activeTab === 'Game History'">
      <h2 class="section-title">Game History</h2>
      <p class="config-intro">
        Saved on disk and kept between sessions so you can review previous rounds, payout issues, and manual follow-up cases.
      </p>

      <div class="history-actions">
        <button type="button" class="copy-btn history-danger-btn" @click="showClearHistoryConfirm = true">Clear History</button>
        <button type="button" class="copy-btn" @click="seedFakeHistory">Seed Fake Data (1000)</button>
      </div>

      <div class="history-search-wrapper">
        <input
          v-model="historySearch"
          type="text"
          class="history-search"
          placeholder="Search by player name"
          @input="onHistorySearchInput"
          @focus="showNameSuggestions = true"
          @blur="hideNameSuggestionsWithDelay"
          autocomplete="off"
        />
        <ul v-if="showNameSuggestions && playerNameSuggestions.length" class="history-suggestions" @mousedown.prevent>
          <li v-for="(name, idx) in playerNameSuggestions" :key="`suggest-${idx}`" @mousedown.prevent="selectHistorySuggestion(name)">{{ name }}</li>
        </ul>
      </div>

      <div class="history-filter-row">
        <label class="history-filter-flagged">
          <input type="checkbox" v-model="showFlaggedOnly" /> Show flagged only
        </label>
      </div>

      <div v-if="filteredGameHistory.length === 0" class="trade-empty">
        No game history matched your search.
      </div>

      <div v-else class="history-list">
        <button
          v-for="entry in filteredGameHistory"
          :key="entry.id"
          type="button"
          class="history-card"
          :class="{ 'history-card-issue': entry.issue }"
          @click="openHistoryEntry(entry)"
        >
          <div class="history-card-top">
            <span class="history-player">{{ entry.playerName || 'Unknown' }}</span>
            <span class="history-status" :class="historyStatusClass(entry)">{{ entry.status || 'Unknown' }}</span>
          </div>
          <div class="history-meta-row">
            <span>{{ entry.game || 'Unknown Game' }}</span>
            <span>{{ formatDateTime(entry.startedAt) }}</span>
          </div>
          <div class="history-meta-row">
            <span>Winner: {{ entry.winner || 'Not Recorded' }}</span>
            <span v-if="entry.issue" class="history-issue-text">Flagged</span>
          </div>
          <div class="history-summary">
            Bet: {{ summarizeTradeItems(entry.betItems) || 'No bet items recorded' }}
          </div>
          <div class="history-summary" v-if="entry.issueReason">
            Issue: {{ entry.issueReason }}
          </div>
          <div class="game-card-action">Click for full details</div>
        </button>
      </div>

      <div class="game-guide-modal-backdrop" v-if="selectedHistory" @click="closeHistoryEntry">
        <div class="game-guide-modal history-modal" @click.stop>
          <div class="game-guide-header">
            <h3 class="section-title game-guide-title">{{ selectedHistory.playerName || 'Unknown' }}</h3>
            <button type="button" class="copy-btn" @click="closeHistoryEntry">Close</button>
          </div>

          <div class="history-detail-grid">
            <div class="history-detail-item">
              <div class="game-guide-label">Game</div>
              <div class="game-guide-text">{{ selectedHistory.game || 'Unknown' }}</div>
            </div>
            <div class="history-detail-item">
              <div class="game-guide-label">Started</div>
              <div class="game-guide-text">{{ formatDateTime(selectedHistory.startedAt) }}</div>
            </div>
            <div class="history-detail-item">
              <div class="game-guide-label">Completed</div>
              <div class="game-guide-text">{{ formatDateTime(selectedHistory.completedAt) || 'Still open / not recorded' }}</div>
            </div>
            <div class="history-detail-item">
              <div class="game-guide-label">Winner</div>
              <div class="game-guide-text">{{ selectedHistory.winner || 'Not Recorded' }}</div>
            </div>
            <div class="history-detail-item">
              <div class="game-guide-label">Status</div>
              <div class="game-guide-text">{{ selectedHistory.status || 'Unknown' }}</div>
            </div>
            <div class="history-detail-item">
              <div class="game-guide-label">Issue</div>
              <div class="game-guide-text">{{ selectedHistory.issue ? (selectedHistory.issueReason || 'Flagged for review') : 'No issue flagged' }}</div>
            </div>
            <div class="history-detail-item">
              <div class="game-guide-label">Player Result</div>
              <div class="game-guide-text">{{ selectedHistory.playerResult || 'Not recorded' }}</div>
            </div>
            <div class="history-detail-item">
              <div class="game-guide-label">Dealer Result</div>
              <div class="game-guide-text">{{ selectedHistory.dealerResult || 'Not recorded' }}</div>
            </div>
          </div>

          <div class="game-guide-block">
            <div class="game-guide-label">Bet Items</div>
            <div class="game-guide-text">{{ summarizeTradeItems(selectedHistory.betItems) || 'No bet items recorded' }}</div>
          </div>
          <div class="game-guide-block">
            <div class="game-guide-label">Payout Items</div>
            <div class="game-guide-text">{{ summarizeTradeItems(selectedHistory.payoutItems) || 'No payout items recorded' }}</div>
          </div>
          <div class="game-guide-block">
            <div class="game-guide-label">Round Notes</div>
            <div v-if="(selectedHistory.notes || []).length === 0" class="game-guide-text">No additional notes recorded.</div>
            <div v-else class="history-notes">
              <div v-for="(note, index) in selectedHistory.notes" :key="`note-${index}`" class="game-guide-text">{{ note }}</div>
            </div>
          </div>
        </div>
      </div>

      <div class="game-guide-modal-backdrop" v-if="showClearHistoryConfirm" @click="closeClearHistoryConfirm">
        <div class="game-guide-modal confirm-modal" @click.stop>
          <div class="game-guide-header">
            <h3 class="section-title game-guide-title">Clear Game History</h3>
          </div>
          <p class="game-guide-text">Are you sure? This will remove all saved game history from the app and delete the persisted history file.</p>
          <div class="confirm-actions">
            <button type="button" class="copy-btn" @click="closeClearHistoryConfirm">Cancel</button>
            <button type="button" class="copy-btn history-danger-btn" @click="confirmClearHistory">Clear Everything</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Stats tab -->
    <div v-if="activeTab === 'Stats'">
      <h2 class="section-title">Casino Stats</h2>
      <div style="display:flex;justify-content:center;gap:8px;margin-bottom:12px;">
        <button :class="['copy-btn', { 'active': statsRangeKey === 'all_time' }]" @click="loadStats('all_time')">All time</button>
        <button :class="['copy-btn', { 'active': statsRangeKey === 'today' }]" @click="loadStats('today')">Today</button>
      </div>
      <div v-if="!activeStats || Object.keys(activeStats || {}).length === 0" class="trade-empty">
        No stats available.
      </div>
      <div v-else>
        <div class="game-guide-block">
          <div class="game-guide-label">Overall</div>
          <div class="game-guide-text">
            Player wins: {{ (activeStats && activeStats.overall && activeStats.overall.playerWins) || 0 }} ({{ formatNumber((activeStats && activeStats.overall && activeStats.overall.playerWinRate) || 0) }}%) —
            Dealer wins: {{ (activeStats && activeStats.overall && activeStats.overall.dealerWins) || 0 }} ({{ formatNumber((activeStats && activeStats.overall && activeStats.overall.dealerWinRate) || 0) }}%) —
            Completed rounds: {{ (activeStats && activeStats.overall && activeStats.overall.completedRounds) || 0 }}
          </div>
        </div>
        <div class="game-guide-block">
          <div class="game-guide-label">By Game</div>
          <table class="catalog-table">
            <thead><tr><th>Game</th><th>Player Wins</th><th>Dealer Wins</th><th>Player %</th><th>Dealer %</th><th>Rounds</th></tr></thead>
            <tbody>
              <tr v-for="g in gameKeys" :key="g">
                <td>{{ (g === 'TriH' || g === 'TriL' || g === 'Tri') ? 'Tri' : (g === 'Other' ? 'Other' : g) }}</td>
                <td>{{ (activeStats && activeStats.byGame && activeStats.byGame[g] && activeStats.byGame[g].playerWins) || 0 }}</td>
                <td>{{ (activeStats && activeStats.byGame && activeStats.byGame[g] && activeStats.byGame[g].dealerWins) || 0 }}</td>
                <td>{{ formatNumber((activeStats && activeStats.byGame && activeStats.byGame[g] && activeStats.byGame[g].playerWinRate) || 0) }}%</td>
                <td>{{ formatNumber((activeStats && activeStats.byGame && activeStats.byGame[g] && activeStats.byGame[g].dealerWinRate) || 0) }}%</td>
                <td>{{ (activeStats && activeStats.byGame && activeStats.byGame[g] && activeStats.byGame[g].completedRounds) || 0 }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <div v-if="activeTab === 'Logs'">
      <div class="log-grid">
        <div>
          <div class="log-title-row">
            <h2 class="section-title">Activity Log</h2>
            <button class="copy-btn" @click="copyActivityLogs">Copy</button>
          </div>
          <div id="log" ref="logbox" class="log-section">
            <div v-for="(msg, index) in log" :key="`roll-${index}`">{{ msg }}</div>
          </div>
        </div>
        <div>
          <div class="log-title-row">
            <h2 class="section-title">Chat Logs</h2>
            <button class="copy-btn" @click="copyChatLogs">Copy</button>
          </div>
          <div ref="chatlogbox" class="log-section chat-log-section">
            <div v-for="(msg, index) in chatLog" :key="`chat-${index}`">{{ msg }}</div>
          </div>
        </div>
        <div>
          <div class="log-title-row">
            <h2 class="section-title">Debugging</h2>
            <button class="copy-btn" @click="copyDebugLogs">Copy</button>
          </div>
          <div ref="debuglogbox" class="log-section debug-log-section">
            <div v-for="(msg, index) in debugLog" :key="`debug-${index}`">{{ msg }}</div>
          </div>
            <!-- Python parser debug output removed -->
        </div>
      </div>

      <hr class="trade-divider" />

      <h2 class="section-title">Event Browser</h2>
      <div class="history-actions">
        <button class="copy-btn" @click="loadEventDates">Refresh Dates</button>
      </div>
      <div class="event-browser" style="margin-top:8px;">
        <div style="display:flex;gap:8px;align-items:center;">
          <div class="game-guide-label">Date</div>
          <select v-model="selectedEventDate" @change="loadEventsForDate(selectedEventDate)">
            <option value="">Select date</option>
            <option v-for="d in eventDates" :key="d" :value="d">{{ d }}</option>
          </select>
          <button class="copy-btn" :disabled="!selectedEventDate" @click="downloadEventsForDate">Download</button>
        </div>
        <div class="trade-empty" style="margin-top:8px;">Click a date and press Download to open events in Notepad.</div>
      </div>

      <h2 class="section-title">Room Identity Map (G_USERS)</h2>
      <div v-if="roomIdentity.length === 0" class="trade-empty">
        No room users decoded yet.<br />
        <span style="font-size:12px;color:#666">Updates when users join/leave and on periodic G_USRS refresh.</span>
      </div>
      <table v-else class="catalog-table">
        <thead><tr><th>Name</th><th>Short</th><th>Chat ID</th><th>Room ID</th><th>Token</th></tr></thead>
        <tbody>
          <tr v-for="(entry, index) in roomIdentity" :key="`room-id-${index}`">
            <td><span class="catalog-label">{{ entry.name || '-' }}</span></td>
            <td><span class="catalog-label">{{ entry.short || '-' }}</span></td>
            <td><span class="catalog-label">{{ entry.chatIndex > 0 ? entry.chatIndex : '-' }}</span></td>
            <td><span class="catalog-label">{{ entry.roomIndex > 0 ? entry.roomIndex : '-' }}</span></td>
            <td><span class="catalog-label">{{ entry.token || '-' }}</span></td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="activeTab === 'Raffle'">
      <h2 class="section-title">Raffle Tickets</h2>

      <div class="game-guide-block">
        <div class="game-guide-label">Raffle Name</div>
        <input v-model="raffleNameInput" type="text" placeholder="Enter raffle name" style="width:100%;max-width:420px;" />
        <div class="game-guide-label" style="margin-top:8px;">Prize</div>
        <input v-model="rafflePrizeNameInput" type="text" placeholder="Prize name" style="width:70%;max-width:300px;display:inline-block;" />
        <input v-model.number="rafflePrizeCountInput" type="number" min="1" style="width:80px;margin-left:8px;display:inline-block;" />
      </div>

      <div class="game-guide-block" style="margin-top:8px;">
        <button class="copy-btn" @click="raffleInfo.active ? stopRaffle() : startRaffle()">{{ raffleInfo.active ? 'End Raffle' : 'Start Raffle' }}</button>
        <button v-if="!raffleInfo.active && (Object.keys(raffleTickets).length>0 || raffleInfo.name)" class="copy-btn" @click="resumeRaffle" style="margin-left:8px;">Resume Raffle</button>
        <button v-if="!raffleInfo.active && raffleInfo.endAt" class="copy-btn" @click="drawWinner" style="margin-left:8px;">Draw Winner</button>
        <button class="copy-btn" @click="resetRaffle" style="margin-left:8px;">New Raffle</button>
      </div>

      <div class="game-guide-block" style="margin-top:8px;">
        <div class="game-guide-label">Status</div>
        <div class="game-guide-text">{{ raffleInfo.active ? 'Active' : 'Inactive' }} — {{ raffleInfo.name || '—' }}</div>
        <div class="game-guide-text">Prize: {{ raffleInfo.prizeName || '—' }} x{{ raffleInfo.prizeCount || 0 }}</div>
        <div class="game-guide-text">Started: {{ raffleInfo.startAt || '—' }} Ended: {{ raffleInfo.endAt || '—' }}</div>
      </div>

      <div v-if="Object.keys(raffleTickets).length === 0" class="trade-empty">No raffle tickets recorded.</div>
      <table v-else class="catalog-table">
        <thead><tr><th>Player</th><th>Tickets</th></tr></thead>
        <tbody>
          <tr v-for="(t, p) in raffleTickets" :key="p">
            <td><span class="catalog-label">{{ p }}</span></td>
            <td><span class="catalog-label">{{ t }}</span></td>
          </tr>
        </tbody>
      </table>

      <h3>Recent Contributions</h3>
      <ul>
        <li v-for="c in raffleContributions" :key="c.at">{{ c.at }} — {{ c.player }}: {{ c.tickets }} tickets ({{ c.items.length }} items)</li>
      </ul>
    </div>

    <!-- Archived raffles modal -->
    <div class="dice-setup-modal-backdrop" v-if="showArchivedModal" @click="showArchivedModal = false">
      <div class="dice-setup-modal" @click.stop>
        <div class="game-guide-header">
          <h3 class="section-title game-guide-title">Archived Raffles</h3>
          <button type="button" class="copy-btn" @click="showArchivedModal = false">Close</button>
        </div>
        <div style="max-height:400px;overflow:auto;padding:8px;">
          <input v-model="archivedSearch" placeholder="Search archived raffles..." style="width:100%;padding:8px;border-radius:4px;border:1px solid #ccc;margin-bottom:8px;" />
          <div v-if="(!archivedList || archivedList.length === 0)">No archived raffles found.</div>
          <ul v-else style="list-style:none;padding:0;margin:0;">
            <li v-for="entry in filteredArchivedList" :key="entry.index" style="padding:6px;border-bottom:1px solid rgba(255,255,255,0.03);display:flex;justify-content:space-between;align-items:center;">
              <div style="flex:1;">{{ entry.index }}: {{ entry.name || '—' }} — {{ entry.startAt || '—' }} to {{ entry.endAt || '—' }}</div>
              <div style="display:flex;gap:8px;margin-left:12px;">
                <button class="copy-btn" @click="loadArchivedByIndex(entry.index)">Load</button>
                <button class="copy-btn" @click="loadAndResumeByIndex(entry.index)">Load & Resume</button>
              </div>
            </li>
          </ul>
        </div>
      </div>
    </div>

  <!-- Users bottom bar removed per user request -->

  <div id="packet-tooltip" v-if="tooltipVisible" class="packet-tooltip" v-html="tooltipHtml" :style="{ left: tooltipLeft + 'px', top: tooltipTop + 'px' }"></div>
  </div>
</template>

<script>
export default {
  data() {
    return {
      activeTab: 'Home',
      activeGameGuide: null,
      gameGuides: [
        {
          key: 'poker',
          title: 'Poker',
          summary: 'Five dice poker hand showdown with automatic dealer response.',
          description: 'Poker uses five dice and compares the player hand against the dealer hand after the trade is completed. The higher poker hand wins the round.',
          howItWorks: 'The app asks which game the player wants, waits for Poker, then runs the poker sequence, evaluates both hands, and handles the result flow automatically.',
          playerFlow: 'Trade the bet, choose Poker in chat when prompted, then wait for the player and dealer rolls to finish.',
          dealerFlow: 'The dealer records the bet, starts the poker sequence, calculates which hand wins, and prepares payout handling if the player beats the dealer.',
        },
        {
          key: '21',
          title: '21',
          summary: 'Automatic dice roll aiming for a strong total without going too low.',
          description: '21 is a fast internal dice game where the app rolls and evaluates the result automatically. The side with the better valid 21 result wins the round.',
          howItWorks: 'After trade completion the player chooses 21, the app starts the 21 roll flow, compares totals, and announces the result through the normal game pipeline.',
          playerFlow: 'Trade the bet, say 21 when prompted, and let the roll complete.',
          dealerFlow: 'The dealer starts the 21 routine, tracks the totals, decides who won the round, and continues into payout handling if the player wins.',
        },
        {
          key: '13',
          title: '13',
          summary: 'Automatic dice roll flow tuned for the 13 game rules.',
          description: '13 runs as its own internal dice routine after the player picks it from chat. The better valid 13 result wins the round.',
          howItWorks: 'The app listens for the 13 selection, starts the dedicated 13 rolling logic, compares the outcome, then processes the winner the same way as other games.',
          playerFlow: 'Trade the bet, say 13 when prompted, and wait for the roll and outcome.',
          dealerFlow: 'The dealer starts the 13 routine, calculates who won, and manages the rest of the round automatically.',
        },
        {
          key: 'tri',
          title: 'Tri',
          summary: 'Three-dice formation roll available through command flow.',
          description: 'Tri is a command-driven roll mode using three dice instead of the trade-selected round flow.',
          howItWorks: 'It is triggered from chat commands and rolls three dice in the configured formation. This is a utility roll mode rather than the main trade-game flow.',
          playerFlow: 'Use the Tri command when needed and let the app roll the three dice.',
          dealerFlow: 'The dealer/app handles the roll output and result logging automatically, but the house rules for who wins depend on how you are using the tri command.',
        },
        
      ],
      tradeItems: [],
      activeGameBetItems: [],
      ownTradeItems: [],
      handItems: [],
      gameHistory: [],
      historySearch: '',
      selectedHistory: null,
      showClearHistoryConfirm: false,
      showDiceSetupModal: false,
      showDealerNameModal: false,
      dealerNameInput: '',
      roomNameInput: '',
      maxUniqueItemsInput: 5,
      maxQuantityPerItemInput: 50,
      diceSetup: [],
      casinoStatus: 'Stopped',
      casinoStatusKey: 'stopped',
      casinoStats: null,
      casinoStatsToday: null,
      casinoStatsMap: {},
      statsRangeKey: 'all_time',
      roomIdentity: [],
      log: [],
      debugLog: [],
      chatLog: [],
      raffleTickets: {},
      raffleContributions: [],
      raffleWinner: '',
      raffleInfo: { active: false, name: '', prizeName: '', prizeCount: 0, startAt: '', endAt: '' },
      raffleNameInput: '',
      rafflePrizeNameInput: '',
      rafflePrizeCountInput: 1,
      showArchivedModal: false,
      archivedList: [],
      archivedSearch: '',
      showNameSuggestions: false,
      showFlaggedOnly: false,
      // Auto shout UI state
      autoShoutPhrase: '',
      autoShoutSeconds: 30,
      autoShoutEnabled: false,
      autoShoutPresets: [],
      // Auto shout #2 UI state
      autoShoutPhrase2: '',
      autoShoutSeconds2: 30,
      autoShoutEnabled2: false,
      autoShoutPresets2: [],
      // Dealer open UI state
      dealerOpenEnabled: true,
      dealerTradeSeconds: 45,
      dealerAnnounceSeconds: 45,
      // Dealer mode: when true, only Under/Over-7 is presented to players
      onlyUnderOverMode: false,
      // UI toggle: enable Under/Over-7 mode (allow '7' payout multiplier)
      underOver7Mode: false,
      // Dealer-selected UO7 payout multiplier (2..5)
      uo7Multiplier: 3,
      // Risk mode: when true, enable Risk banking mechanic
      riskModeEnabledInput: false,
      // Block recommended-rooms packet
      blockRecommendedRooms: true,
      // Block slide-object-bundle packet
      blockSlideObjectBundle: true,
      // Outgoing block flags
      blockRecommendedRoomsOutgoing: true,
      blockSlideObjectBundleOutgoing: true,
      // Additional incoming block flags
      blockStatusEffects: true,
      blockRemoveBuddy: true,
      blockFriendListUpdate: true,
      // Favourite room results (incoming) - default disabled
      blockFavouriteRoomResults: false,
      // New incoming packet blocks (enabled by default)
      blockArticlesPage: true,
      blockCalendarEvents: true,
      // Block USER_BANNED (header 35)
      blockUserBanned: true,
      // Block raw 4095 packets (header 4095)
      blockIncoming4095: true,
      // Outgoing poll-event
      blockPollEventEligibilityOutgoing: true,
      // New outgoing packet blocks (enabled by default)
      blockGetPageArticlesOutgoing: true,
      blockGetCalendarEventsOutgoing: true,
      blockFriendListUpdateOutgoing: true,
      // Tooltip state for packet info (fixed, rendered above everything)
      tooltipVisible: false,
      tooltipHtml: '',
      tooltipLeft: 0,
      tooltipTop: 0,
      // Live UI indicators for partner activity
      currentTraderName: '',
      currentGamePlayerName: '',
      // Event log browser state
      eventDates: [],
      selectedEventDate: '',
      eventsForDate: [],
    };
  },
  computed: {
    filteredArchivedList() {
      if (!this.archivedList || this.archivedList.length === 0) return [];
      const q = (this.archivedSearch || '').trim().toLowerCase();
      if (!q) return this.archivedList;
      return this.archivedList.filter(e => {
        const name = (e.name || '').toLowerCase();
        const start = (e.startAt || '').toLowerCase();
        const end = (e.endAt || '').toLowerCase();
        return name.includes(q) || start.includes(q) || end.includes(q) || String(e.index).includes(q);
      });
    },
  },
  computed: {
    tradeItemsWithDisplay() {
      return this.tradeItems.map(item => {
        return { ...item, displayName: this.formatItemName(item.Name) };
      });
    },
    ownTradeItemsWithDisplay() {
      return this.ownTradeItems.map(item => {
        return { ...item, displayName: this.formatItemName(item.Name) };
      });
    },
    handItemsWithDisplay() {
      return this.handItems.map(item => {
        return { ...item, displayName: this.formatItemName(item.Name) };
      });
    },
    payoutRows() {
      const handByName = this.handItems.reduce((acc, item) => {
        acc[item.Name] = (acc[item.Name] || 0) + item.Quantity;
        return acc;
      }, {});

      const liveIncomingByName = this.tradeItems.reduce((acc, item) => {
        acc[item.Name] = (acc[item.Name] || 0) + item.Quantity;
        return acc;
      }, {});

      return this.activeBetItemsWithDisplay.map(item => {
        const required = item.Quantity;
        const payoutTotal = item.Quantity * 2;
        const includeLiveIncoming = this.tradeItems.length > 0 ? (liveIncomingByName[item.Name] || 0) : 0;
        const have = (handByName[item.Name] || 0) + includeLiveIncoming;
        const short = Math.max(required - have, 0);
        return {
          name: item.Name,
          displayName: item.displayName,
          betQty: item.Quantity,
          required,
          payoutTotal,
          have,
          short,
        };
      });
    },
    canCoverPayout() {
      return this.payoutRows.every((row) => row.short === 0);
    },
    activeBetItems() {
      return this.tradeItems.length > 0 ? this.tradeItems : this.activeGameBetItems;
    },
    activeBetItemsWithDisplay() {
      return this.activeBetItems.map(item => {
        return { ...item, displayName: this.formatItemName(item.Name) };
      });
    },
    activeBetSourceLabel() {
      if (this.tradeItems.length > 0) {
        return 'live trade';
      }
      if (this.activeGameBetItems.length > 0) {
        return 'current round';
      }
      return '';
    },
    filteredGameHistory() {
      const q = this.historySearch.trim().toLowerCase();
      let list = this.gameHistory || [];
      if (this.showFlaggedOnly) {
        list = list.filter((entry) => entry && entry.issue);
      }
      if (!q) {
        return list;
      }
      return list.filter((entry) => String(entry.playerName || '').toLowerCase().includes(q));
    },
    playerNameSuggestions() {
      const q = String(this.historySearch || '').trim().toLowerCase();
      const namesSet = new Set();
      let base = (this.gameHistory || []);
      if (this.showFlaggedOnly) {
        base = base.filter((entry) => entry && entry.issue);
      }
      base.forEach((entry) => {
        if (entry && entry.playerName) {
          namesSet.add(String(entry.playerName));
        }
      });
      const names = Array.from(namesSet).sort((a, b) => a.localeCompare(b));
      if (!q) {
        return names.slice(0, 10);
      }
      return names.filter(n => n.toLowerCase().includes(q)).slice(0, 10);
    },
    activeUsers() {
      return (this.roomIdentity || []).filter((u) => {
        return this.isUserTrading(u) || this.isUserInGame(u);
      });
    },
    activeStats() {
      return this.casinoStatsMap[this.statsRangeKey] || {};
    },
    gameKeys() {
      const keys = (this.activeStats && this.activeStats.byGame) ? Object.keys(this.activeStats.byGame) : [];
      const preferred = ['Poker', '21', '13', 'Tri'];
      const presentPreferred = preferred.filter(k => keys.includes(k));
      const rest = keys.filter(k => !preferred.includes(k)).sort();
      return presentPreferred.concat(rest);
    },
  },
  methods: {
    openGameGuide(game) {
      this.activeGameGuide = game;
    },
    closeGameGuide() {
      this.activeGameGuide = null;
    },
      async startCasino() {
        try {
          if (this.casinoStatusKey === 'stopped') {
              // preserve previously entered dealer name and limits if present,
              // otherwise ensure sane defaults are set before showing modal
              if (!this.dealerNameInput) this.dealerNameInput = '';
              if (!this.maxUniqueItemsInput || this.maxUniqueItemsInput < 1) this.maxUniqueItemsInput = 5;
              if (!this.maxQuantityPerItemInput || this.maxQuantityPerItemInput < 1) this.maxQuantityPerItemInput = 50;
              this.showDealerNameModal = true;
            return;
          }
          this.addLogMsg('[UI] Casino already started');
        } catch (err) {
          this.addLogMsg('[UI] Failed to start dice setup');
          console.error(err);
        }
      },

      async confirmDealerName() {
        try {
          const name = (this.dealerNameInput || '').trim();
          const roomName = (this.roomNameInput || '').trim();
          const maxUnique = Number(this.maxUniqueItemsInput || 0);
          const maxPer = Number(this.maxQuantityPerItemInput || 0);

          if (!name) {
            this.addLogMsg('[UI] Start cancelled: no dealer name provided');
            return;
          }

          if (!roomName) {
            this.addLogMsg('[UI] Start cancelled: no room name provided');
            return;
          }

          if (!Number.isInteger(maxUnique) || maxUnique < 1) {
            this.addLogMsg('[UI] Start cancelled: max unique items must be a positive integer');
            return;
          }

          if (!Number.isInteger(maxPer) || maxPer < 1) {
            this.addLogMsg('[UI] Start cancelled: max quantity per item must be a positive integer');
            return;
          }

          // send dealer mode to backend before starting setup
          await window.go.main.App.SetOnlyUnderOver(this.onlyUnderOverMode);
          // set the UO7 payout multiplier before enabling mode / starting
          await window.go.main.App.SetUnderOver7PayoutMultiplier(this.uo7Multiplier);
          await window.go.main.App.SetUnderOver7Mode(this.underOver7Mode);
          await window.go.main.App.StartCasinoSetup(name, roomName, maxUnique, maxPer, this.riskModeEnabledInput);

          this.showDealerNameModal = false;
          const expected = (this.onlyUnderOverMode || this.underOver7Mode) ? 2 : 5;
          this.diceSetup = Array.from({ length: expected }).map(() => ({ rolled: false, id: 0, value: 0 }));
          this.showDiceSetupModal = true;
          this.casinoStatus = 'Awaiting dice rolls';
          this.casinoStatusKey = 'awaiting';
          this.addLogMsg(`[UI] Dice setup started; roll all ${expected} dice`);
        } catch (err) {
          this.addLogMsg('[UI] Failed to start dice setup');
          console.error(err);
          this.showDealerNameModal = false;
        }
      },

      cancelDealerName() {
        this.showDealerNameModal = false;
        this.addLogMsg('[UI] Start cancelled by user');
      },

      // pause/resume removed — simplified start/stop control

      async stopCasino() {
        try {
          await window.go.main.App.StopCasinoSetup();
          this.casinoStatus = 'Stopped';
          this.casinoStatusKey = 'stopped';
          this.showDiceSetupModal = false;
          this.diceSetup = [];
          this.addLogMsg('[UI] Casino stopped');
        } catch (err) {
          this.addLogMsg('[UI] Failed to stop casino');
          console.error(err);
        }
      },
    openHistoryEntry(entry) {
      this.selectedHistory = entry;
    },
    closeHistoryEntry() {
      this.selectedHistory = null;
    },
    closeClearHistoryConfirm() {
      this.showClearHistoryConfirm = false;
    },
    onHistorySearchInput() {
      this.showNameSuggestions = true;
    },
    seedFakeHistory() {
      const names = [
        'Alex','Sam','Taylor','Jordan','Casey','Riley','Jamie','Morgan','Cameron','Avery',
        'Hayden','Parker','Quinn','Rowan','Dakota','Skyler','Reese','Marley','Sasha','Eli',
        'Jesse','Kris','Logan','Charlie','Blake','Devin','Drew','Finley','Emerson','Harper',
        'Kai','Luca','Nico','Noel','Owen','Paige','Remy','Rory','Soren','Toby',
        'Violet','Will','Zara','Yuri','Ira','Mina','Gabe','Ivy','Brad','Nate'
      ];
      const games = ['21','13','poker','tri','roll'];
      const now = Date.now();
      const rows = [];
      for (let i = 0; i < 1000; i++) {
        const playerName = names[Math.floor(Math.random() * names.length)];
        const game = games[Math.floor(Math.random() * games.length)];
        const startedAt = new Date(now - Math.floor(Math.random() * 1000 * 60 * 60 * 24 * 365)).toISOString();
        const completedAt = new Date(Date.parse(startedAt) + Math.floor(Math.random() * 1000 * 60 * 60 * 24)).toISOString();
        const playerScore = Math.floor(Math.random() * 21) + 1;
        const dealerScore = Math.floor(Math.random() * 21) + 1;
        const winner = playerScore >= dealerScore ? playerName : 'Dealer';
        const betItems = [];
        const betCount = Math.floor(Math.random() * 3);
        for (let j = 0; j < betCount; j++) {
          betItems.push({ Name: 'coin', Quantity: Math.floor(Math.random() * 10) + 1 });
        }
        const entry = {
          id: `fake-${i}-${now}`,
          playerName,
          game,
          startedAt,
          completedAt,
          winner,
          status: 'Completed',
          betItems,
          payoutItems: [],
          notes: [],
          issue: Math.random() < 0.02,
          issueReason: Math.random() < 0.02 ? 'Flagged test' : undefined,
          playerResult: String(playerScore),
          dealerResult: String(dealerScore),
        };
        rows.push(entry);
      }
      // Ensure some flagged entries exist for testing
      const flaggedCount = 25;
      for (let k = 0; k < flaggedCount; k++) {
        const idx = Math.floor(Math.random() * rows.length);
        rows[idx].issue = true;
        rows[idx].issueReason = rows[idx].issueReason || 'Flagged test';
      }
      this.gameHistory = rows;
      this.addLogMsg(`[UI] Seeded ${rows.length} fake history rows`);
    },
      selectHistorySuggestion(name) {
        this.historySearch = name;
        this.showNameSuggestions = false;
      },
      hideNameSuggestionsWithDelay() {
        setTimeout(() => {
          this.showNameSuggestions = false;
        }, 180);
      },
    async confirmClearHistory() {
      try {
        await window.go.main.App.ClearGameHistory();
        this.selectedHistory = null;
        this.historySearch = '';
        this.showClearHistoryConfirm = false;
        this.addLogMsg('[UI] Cleared game history');
      } catch (error) {
        this.addLogMsg('Error clearing game history');
        console.error(error);
      }
    },
    historyStatusClass(entry) {
      if (entry.issue) {
        return 'history-status-issue';
      }
      const status = String(entry.status || '').toLowerCase();
      if (status.includes('completed')) {
        return 'history-status-complete';
      }
      if (status.includes('pending') || status.includes('awaiting')) {
        return 'history-status-pending';
      }
      if (status.includes('result')) {
        return 'history-status-info';
      }
      return 'history-status-info';
    },
    summarizeTradeItems(items) {
      return (items || []).map((item) => `${item.Quantity}x ${this.formatItemName(item.Name)}`).join(', ');
    },
    formatDateTime(value) {
      if (!value) {
        return '';
      }
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) {
        return value;
      }
      return date.toLocaleString();
    },
    async refreshGameHistory() {
      try {
        const jsonStr = await window.go.main.App.GetGameHistoryJSON();
        this.gameHistory = JSON.parse(jsonStr || '[]') || [];
      } catch (error) {
        this.addLogMsg('Error loading game history');
        console.error(error);
      }
    },
    
    addLogMsg(msg) {
      this.log.push(msg);
      this.scrollBox('logbox');
    },
    addChatLogMsg(msg) {
      this.chatLog.push(msg);
      this.scrollBox('chatlogbox');
    },
    async copyTextToClipboard(text, label) {
      try {
        if (navigator && navigator.clipboard && navigator.clipboard.writeText) {
          await navigator.clipboard.writeText(text);
        } else {
          const ta = document.createElement('textarea');
          ta.value = text;
          ta.style.position = 'fixed';
          ta.style.left = '-9999px';
          document.body.appendChild(ta);
          ta.focus();
          ta.select();
          document.execCommand('copy');
          document.body.removeChild(ta);
        }
        this.addLogMsg(`[UI] Copied ${label} to clipboard`);
      } catch (error) {
        this.addLogMsg(`[UI] Failed to copy ${label}`);
        console.error(error);
      }
    },
    async copyActivityLogs() {
      const text = this.log.join('\n');
      await this.copyTextToClipboard(text, 'activity logs');
    },
    async copyChatLogs() {
      const text = this.chatLog.join('\n');
      await this.copyTextToClipboard(text, 'chat logs');
    },
    async copyDebugLogs() {
      const text = this.debugLog.join('\n');
      await this.copyTextToClipboard(text, 'debug logs');
    },
    async loadEventDates() {
      try {
        const json = await window.go.main.App.ListEventDatesJSON();
        this.eventDates = JSON.parse(json || '[]') || [];
      } catch (e) {
        this.addLogMsg('[UI] Failed to load event dates');
        console.error(e);
      }
    },
    async loadEventsForDate(date) {
      try {
        if (!date) return;
        const json = await window.go.main.App.ListEventsForDateJSON(date);
        this.eventsForDate = JSON.parse(json || '[]') || [];
      } catch (e) {
        this.addLogMsg('[UI] Failed to load events for date');
        console.error(e);
      }
    },
    async downloadEventsForDate() {
      try {
        if (!this.selectedEventDate) {
          this.addLogMsg('[UI] No date selected');
          return;
        }
        const path = await window.go.main.App.ExportEventsForDate(this.selectedEventDate);
        if (path) {
          this.addLogMsg(`[UI] Exported events to ${path}`);
        } else {
          this.addLogMsg('[UI] No events found or export failed');
        }
      } catch (e) {
        this.addLogMsg('[UI] Failed to export events');
        console.error(e);
      }
    },
    formatItemName(name) {
      return String(name || '')
        .split('_')
        .filter(Boolean)
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join(' ');
    },
    formatNumber(value, decimals = 2) {
      if (value === null || value === undefined) return '';
      const n = Number(value);
      if (Number.isNaN(n)) return String(value);
      return n.toFixed(decimals);
    },
    formatSigned(value) {
      const n = Number(value || 0);
      if (Number.isNaN(n)) return String(value || '');
      return (n > 0 ? '+' : '') + String(n);
    },
    isNameMatch(entry, name) {
      if (!entry || !entry.name || !name) return false;
      return String(entry.name).trim().toLowerCase() === String(name).trim().toLowerCase();
    },
    isUserTrading(entry) {
      if (!entry) return false;
      if (!this.currentTraderName) return false;
      return this.isNameMatch(entry, this.currentTraderName) && (this.tradeItems && this.tradeItems.length > 0);
    },
    isUserInGame(entry) {
      if (!entry) return false;
      if (!this.currentGamePlayerName) return false;
      return this.isNameMatch(entry, this.currentGamePlayerName) && (this.activeGameBetItems && this.activeGameBetItems.length > 0);
    },
    async loadStats(rangeKey) {
      this.statsRangeKey = rangeKey;
      try {
        const jsonStr = await window.go.main.App.GetCasinoStatsJSON(rangeKey);
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.casinoStatsMap[rangeKey] = parsed;
        if (rangeKey === 'all_time') this.casinoStats = parsed;
        if (rangeKey === 'today') this.casinoStatsToday = parsed;
      } catch (e) {
        this.casinoStatsMap[rangeKey] = {};
      }
    },
    scrollBox(refName) {
      this.$nextTick(() => {
        const box = this.$refs[refName];
        if (box) {
          box.scrollTop = box.scrollHeight;
        }
      });
    },
    setAutoShoutPreset(phrase) {
      try {
        this.autoShoutPhrase = String(phrase || '');
        this.$nextTick(() => {
          const el = document.querySelector('input[placeholder="Enter phrase to shout"]');
          if (el && typeof el.focus === 'function') el.focus();
        });
      } catch (e) {}
    },

    loadAutoShoutPresets() {
      try {
        const raw = localStorage.getItem('autoShoutPresets') || '[]';
        const arr = JSON.parse(raw || '[]') || [];
        if (Array.isArray(arr)) {
          this.autoShoutPresets = arr.filter(p => typeof p === 'string');
        } else {
          this.autoShoutPresets = [];
        }
      } catch (e) {
        this.autoShoutPresets = [];
      }
    },

    saveAutoShoutPresets() {
      try {
        localStorage.setItem('autoShoutPresets', JSON.stringify(this.autoShoutPresets || []));
      } catch (e) {}
    },

    addAutoShoutPreset() {
      try {
        const phrase = String(this.autoShoutPhrase || '').trim();
        if (!phrase) {
          this.addLogMsg('[UI] Cannot add empty preset');
          return;
        }
        if (!Array.isArray(this.autoShoutPresets)) this.autoShoutPresets = [];
        if (this.autoShoutPresets.includes(phrase)) {
          this.addLogMsg('[UI] Preset already exists');
          return;
        }
        this.autoShoutPresets.push(phrase);
        this.saveAutoShoutPresets();
        this.addLogMsg('[UI] Preset added');
      } catch (e) {
        console.error('addAutoShoutPreset', e);
      }
    },

    deleteAutoShoutPreset(idx) {
      try {
        if (!Array.isArray(this.autoShoutPresets)) return;
        const removed = this.autoShoutPresets.splice(idx, 1);
        this.saveAutoShoutPresets();
        this.addLogMsg(`[UI] Removed preset: ${removed && removed[0] ? removed[0] : ''}`);
      } catch (e) {
        console.error('deleteAutoShoutPreset', e);
      }
    },

    async saveAutoShout() {
      try {
        await window.go.main.App.SaveAutoShoutConfig(this.autoShoutPhrase || '', Number(this.autoShoutSeconds || 30));
        this.addLogMsg('[UI] AutoShout config saved');
      } catch (e) {
        this.addLogMsg('[UI] Failed to save autoShout config');
        console.error(e);
      }
    },
    async saveDealerOpen() {
      try {
        const cfg = await window.go.main.App.SaveDealerOpenConfig(
          !!this.dealerOpenEnabled,
          Number(this.dealerTradeSeconds || 45),
          Number(this.dealerAnnounceSeconds || 45)
        );

        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.dealerOpenEnabled = !!parsed.enabled;
            this.dealerTradeSeconds = parsed.tradeSeconds || parsed.seconds || 45;
            this.dealerAnnounceSeconds = parsed.announceSeconds || parsed.seconds || 45;
          } catch (e) {}
        } else if (cfg) {
          this.dealerOpenEnabled = !!cfg.enabled;
          this.dealerTradeSeconds = cfg.tradeSeconds || cfg.seconds || 45;
          this.dealerAnnounceSeconds = cfg.announceSeconds || cfg.seconds || 45;
        }
        this.addLogMsg('[UI] Dealer Open config saved');
      } catch (e) {
        this.addLogMsg('[UI] Failed to save Dealer Open config');
        console.error(e);
      }
    },
    async toggleAutoShout() {
      try {
        const next = !this.autoShoutEnabled;
        const cfg = await window.go.main.App.ToggleAutoShout(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.autoShoutEnabled = !!parsed.enabled;
            this.autoShoutPhrase = parsed.phrase || '';
            this.autoShoutSeconds = parsed.seconds || 30;
          } catch (e) {}
        } else if (cfg) {
          this.autoShoutEnabled = !!cfg.enabled;
          this.autoShoutPhrase = cfg.phrase || '';
          this.autoShoutSeconds = cfg.seconds || 30;
        }
        this.addLogMsg('[UI] AutoShout toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle autoShout');
        console.error(e);
      }
    },
    // --- AutoShout slot #2 methods ---
    setAutoShoutPreset2(phrase) {
      try {
        this.autoShoutPhrase2 = String(phrase || '');
        this.$nextTick(() => {
          const els = document.querySelectorAll('input[placeholder="Enter phrase to shout"]');
          const el = (els && els.length > 1) ? els[1] : (els[0] || null);
          if (el && typeof el.focus === 'function') el.focus();
        });
      } catch (e) {}
    },

    loadAutoShoutPresets2() {
      try {
        const raw = localStorage.getItem('autoShout2Presets') || '[]';
        const arr = JSON.parse(raw || '[]') || [];
        if (Array.isArray(arr)) {
          this.autoShoutPresets2 = arr.filter(p => typeof p === 'string');
        } else {
          this.autoShoutPresets2 = [];
        }
      } catch (e) {
        this.autoShoutPresets2 = [];
      }
    },

    saveAutoShoutPresets2() {
      try {
        localStorage.setItem('autoShout2Presets', JSON.stringify(this.autoShoutPresets2 || []));
      } catch (e) {}
    },

    addAutoShoutPreset2() {
      try {
        const phrase = String(this.autoShoutPhrase2 || '').trim();
        if (!phrase) {
          this.addLogMsg('[UI] Cannot add empty preset (slot 2)');
          return;
        }
        if (!Array.isArray(this.autoShoutPresets2)) this.autoShoutPresets2 = [];
        if (this.autoShoutPresets2.includes(phrase)) {
          this.addLogMsg('[UI] Preset already exists (slot 2)');
          return;
        }
        this.autoShoutPresets2.push(phrase);
        this.saveAutoShoutPresets2();
        this.addLogMsg('[UI] Preset added (slot 2)');
      } catch (e) {
        console.error('addAutoShoutPreset2', e);
      }
    },

    deleteAutoShoutPreset2(idx) {
      try {
        if (!Array.isArray(this.autoShoutPresets2)) return;
        const removed = this.autoShoutPresets2.splice(idx, 1);
        this.saveAutoShoutPresets2();
        this.addLogMsg(`[UI] Removed preset (slot 2): ${removed && removed[0] ? removed[0] : ''}`);
      } catch (e) {
        console.error('deleteAutoShoutPreset2', e);
      }
    },

    async saveAutoShout2() {
      try {
        await window.go.main.App.SaveAutoShoutConfig2(this.autoShoutPhrase2 || '', Number(this.autoShoutSeconds2 || 30));
        this.addLogMsg('[UI] AutoShout #2 config saved');
      } catch (e) {
        this.addLogMsg('[UI] Failed to save autoShout #2 config');
        console.error(e);
      }
    },

    async toggleAutoShout2() {
      try {
        const next = !this.autoShoutEnabled2;
        const cfg = await window.go.main.App.ToggleAutoShout2(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.autoShoutEnabled2 = !!parsed.enabled;
            this.autoShoutPhrase2 = parsed.phrase || '';
            this.autoShoutSeconds2 = parsed.seconds || 30;
          } catch (e) {}
        } else if (cfg) {
          this.autoShoutEnabled2 = !!cfg.enabled;
          this.autoShoutPhrase2 = cfg.phrase || '';
          this.autoShoutSeconds2 = cfg.seconds || 30;
        }
        this.addLogMsg('[UI] AutoShout #2 toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle autoShout #2');
        console.error(e);
      }
    },
    async toggleBlockRecommendedRooms() {
      try {
        const next = !!this.blockRecommendedRooms;
        const cfg = await window.go.main.App.ToggleBlockRecommendedRooms(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockRecommendedRooms = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockRecommendedRooms = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block recommended-rooms toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block recommended-rooms');
        console.error(e);
      }
    },
    async toggleBlockRecommendedRoomsOutgoing() {
      try {
        const next = !!this.blockRecommendedRoomsOutgoing;
        const cfg = await window.go.main.App.ToggleBlockRecommendedRoomsOutgoing(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockRecommendedRoomsOutgoing = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockRecommendedRoomsOutgoing = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block recommended-rooms (outgoing) toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block recommended-rooms (outgoing)');
        console.error(e);
      }
    },
    async toggleBlockSlideObjectBundle() {
      try {
        const next = !!this.blockSlideObjectBundle;
        const cfg = await window.go.main.App.ToggleBlockSlideObjectBundle(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockSlideObjectBundle = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockSlideObjectBundle = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block slide-object-bundle toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block slide-object-bundle');
        console.error(e);
      }
    },
    async toggleBlockSlideObjectBundleOutgoing() {
      try {
        const next = !!this.blockSlideObjectBundleOutgoing;
        const cfg = await window.go.main.App.ToggleBlockSlideObjectBundleOutgoing(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockSlideObjectBundleOutgoing = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockSlideObjectBundleOutgoing = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block slide-object-bundle (outgoing) toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block slide-object-bundle (outgoing)');
        console.error(e);
      }
    },
    async toggleBlockStatusEffects() {
      try {
        const next = !!this.blockStatusEffects;
        const cfg = await window.go.main.App.ToggleBlockStatusEffects(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockStatusEffects = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockStatusEffects = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block status-effects toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block status-effects');
        console.error(e);
      }
    },
    async toggleBlockRemoveBuddy() {
      try {
        const next = !!this.blockRemoveBuddy;
        const cfg = await window.go.main.App.ToggleBlockRemoveBuddy(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockRemoveBuddy = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockRemoveBuddy = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block remove-buddy toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block remove-buddy');
        console.error(e);
      }
    },
    async toggleBlockFriendListUpdate() {
      try {
        const next = !!this.blockFriendListUpdate;
        const cfg = await window.go.main.App.ToggleBlockFriendListUpdate(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockFriendListUpdate = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockFriendListUpdate = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block friend-list-update toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block friend-list-update');
        console.error(e);
      }
    },
    async toggleBlockPollEventEligibilityOutgoing() {
      try {
        const next = !!this.blockPollEventEligibilityOutgoing;
        const cfg = await window.go.main.App.ToggleBlockPollEventEligibilityOutgoing(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockPollEventEligibilityOutgoing = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockPollEventEligibilityOutgoing = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block poll-event-eligibility (outgoing) toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block poll-event-eligibility (outgoing)');
        console.error(e);
      }
    },
    async toggleBlockArticlesPage() {
      try {
        const next = !!this.blockArticlesPage;
        const cfg = await window.go.main.App.ToggleBlockArticlesPage(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockArticlesPage = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockArticlesPage = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block articles-page toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block articles-page');
        console.error(e);
      }
    },
    async toggleBlockFavouriteRoomResults() {
      try {
        const next = !!this.blockFavouriteRoomResults;
        const cfg = await window.go.main.App.ToggleBlockFavouriteRoomResults(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockFavouriteRoomResults = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockFavouriteRoomResults = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block favourite-room-results toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block favourite-room-results');
        console.error(e);
      }
    },
    async toggleBlockCalendarEvents() {
      try {
        const next = !!this.blockCalendarEvents;
        const cfg = await window.go.main.App.ToggleBlockCalendarEvents(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockCalendarEvents = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockCalendarEvents = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block calendar-events toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block calendar-events');
        console.error(e);
      }
    },
    async toggleBlockUserBanned() {
      try {
        const next = !!this.blockUserBanned;
        const cfg = await window.go.main.App.ToggleBlockUserBanned(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockUserBanned = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockUserBanned = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block user-banned toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block user-banned');
        console.error(e);
      }
    },
    async toggleBlockIncoming4095() {
      try {
        const next = !!this.blockIncoming4095;
        const cfg = await window.go.main.App.ToggleBlockIncoming4095(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockIncoming4095 = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockIncoming4095 = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block incoming 4095 toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block incoming 4095');
        console.error(e);
      }
    },
    async toggleBlockGetPageArticlesOutgoing() {
      try {
        const next = !!this.blockGetPageArticlesOutgoing;
        const cfg = await window.go.main.App.ToggleBlockGetPageArticlesOutgoing(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockGetPageArticlesOutgoing = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockGetPageArticlesOutgoing = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block get-page-articles (outgoing) toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block get-page-articles (outgoing)');
        console.error(e);
      }
    },
    async toggleBlockGetCalendarEventsOutgoing() {
      try {
        const next = !!this.blockGetCalendarEventsOutgoing;
        const cfg = await window.go.main.App.ToggleBlockGetCalendarEventsOutgoing(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockGetCalendarEventsOutgoing = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockGetCalendarEventsOutgoing = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block get-calendar-events (outgoing) toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block get-calendar-events (outgoing)');
        console.error(e);
      }
    },
    async toggleBlockFriendListUpdateOutgoing() {
      try {
        const next = !!this.blockFriendListUpdateOutgoing;
        const cfg = await window.go.main.App.ToggleBlockFriendListUpdateOutgoing(next);
        if (typeof cfg === 'string') {
          try {
            const parsed = JSON.parse(cfg || '{}') || {};
            this.blockFriendListUpdateOutgoing = !!parsed.enabled;
          } catch (e) {}
        } else if (cfg) {
          this.blockFriendListUpdateOutgoing = !!cfg.enabled;
        }
        this.addLogMsg('[UI] Block friendlist-update (outgoing) toggled');
      } catch (e) {
        this.addLogMsg('[UI] Failed to toggle block friendlist-update (outgoing)');
        console.error(e);
      }
    },
    showPacketTooltip(e) {
      try {
        const el = e.currentTarget || e.target;
        const textEl = el.querySelector && el.querySelector('.tooltip');
        const text = textEl ? textEl.innerHTML : (el.getAttribute && el.getAttribute('aria-label')) || '';
        this.tooltipHtml = text || '';
        this.tooltipVisible = true;
        this.$nextTick(() => {
          const tipEl = document.getElementById('packet-tooltip');
          if (!tipEl) return;
          const rect = el.getBoundingClientRect();
          const tipRect = tipEl.getBoundingClientRect();
          let left = rect.left + (rect.width / 2) - (tipRect.width / 2);
          let top = rect.top - tipRect.height - 10;
          if (top < 8) top = rect.bottom + 10;
          const vw = window.innerWidth || document.documentElement.clientWidth;
          if (left < 8) left = 8;
          if (left + tipRect.width > vw - 8) left = vw - tipRect.width - 8;
          this.tooltipLeft = Math.round(left);
          this.tooltipTop = Math.round(top);
        });
      } catch (err) {
        this.tooltipVisible = false;
        this.tooltipHtml = '';
      }
    },
    hidePacketTooltip() {
      this.tooltipVisible = false;
      this.tooltipHtml = '';
    },
    async loadRaffleState() {
      try {
        const s = await window.go.main.App.GetRaffleStateJSON();
        const parsed = JSON.parse(s || '{}') || {};
        this.raffleTickets = parsed.tickets || {};
        this.raffleContributions = parsed.contributions || [];
        this.raffleInfo = {
          active: !!parsed.active,
          name: parsed.name || '',
          prizeName: parsed.prizeName || '',
          prizeCount: parsed.prizeCount || 0,
          startAt: parsed.startAt || '',
          endAt: parsed.endAt || ''
        };
        if (!this.raffleNameInput) this.raffleNameInput = this.raffleInfo.name || '';
        if (!this.rafflePrizeNameInput) this.rafflePrizeNameInput = this.raffleInfo.prizeName || '';
        this.rafflePrizeCountInput = this.raffleInfo.prizeCount || 1;
      } catch (e) {
        this.raffleTickets = {};
        this.raffleContributions = [];
      }
    },

    async drawWinner() {
      try {
        const winner = await window.go.main.App.DrawRaffleWinner(0);
        this.raffleWinner = winner || '';
        if (this.raffleWinner) {
          this.addLogMsg(`[UI] Raffle winner: ${this.raffleWinner}`);
        } else {
          this.addLogMsg('[UI] Raffle draw returned no winner');
        }
      } catch (e) {
        this.addLogMsg('[UI] Raffle draw failed');
        console.error(e);
      }
    },

    async startRaffle() {
      try {
        await window.go.main.App.StartRaffle(this.raffleNameInput || 'Raffle', this.rafflePrizeNameInput || 'Prize', Number(this.rafflePrizeCountInput) || 1);
        await this.loadRaffleState();
        this.addLogMsg('[UI] raffle started');
      } catch (e) {
        this.addLogMsg('[UI] raffle start failed');
        console.error(e);
      }
    },

    async stopRaffle() {
      try {
        await window.go.main.App.StopRaffle();
        await this.loadRaffleState();
        this.addLogMsg('[UI] raffle stopped');
      } catch (e) {
        this.addLogMsg('[UI] raffle stop failed');
        console.error(e);
      }
    },

    async resumeRaffle() {
      try {
        await window.go.main.App.ResumeRaffle();
        await this.loadRaffleState();
        this.addLogMsg('[UI] raffle resumed');
      } catch (e) {
        this.addLogMsg('[UI] raffle resume failed');
        console.error(e);
      }
    },

    async resetRaffle() {
      try {
        await window.go.main.App.ResetRaffle();
        this.raffleTickets = {};
        this.raffleContributions = [];
        this.raffleWinner = '';
        // Clear input fields for a new raffle
        this.raffleNameInput = '';
        this.rafflePrizeNameInput = '';
        this.rafflePrizeCountInput = 1;
        this.addLogMsg('[UI] New raffle created');
      } catch (e) {
        this.addLogMsg('[UI] Raffle reset failed');
        console.error(e);
      }
    },

    async openArchivedModal() {
      try {
        const list = await window.go.main.App.ListArchivedRaffleSummaries();
        // Ensure JS objects have expected keys
        this.archivedList = (list || []).map(e => ({ index: e.index, name: e.name, prizeName: e.prizeName, prizeCount: e.prizeCount, startAt: e.startAt, endAt: e.endAt, active: e.active, ticketsTotal: e.ticketsTotal, contributionsCount: e.contributionsCount }));
        this.archivedSearch = '';
        this.showArchivedModal = true;
      } catch (e) {
        this.addLogMsg('[UI] Failed to list archived raffles');
        console.error(e);
      }
    },

    async loadArchivedByIndex(index) {
      try {
        await window.go.main.App.LoadArchivedRaffleIndex(index);
        await this.loadRaffleState();
        this.showArchivedModal = false;
        this.addLogMsg(`[UI] Loaded archived raffle ${index}`);
      } catch (e) {
        this.addLogMsg('[UI] Failed to load archived raffle');
        console.error(e);
      }
    },

    async loadAndResumeByIndex(index) {
      try {
        await window.go.main.App.LoadArchivedRaffleIndex(index);
        await window.go.main.App.ResumeRaffle();
        await this.loadRaffleState();
        this.showArchivedModal = false;
        this.addLogMsg(`[UI] Loaded and resumed archived raffle ${index}`);
      } catch (e) {
        this.addLogMsg('[UI] Failed to load & resume archived raffle');
        console.error(e);
      }
    },
  },
  async mounted() {
    await this.refreshGameHistory();
      // Fetch minimal stats for ranges
      await this.loadStats('all_time');
      await this.loadStats('today');
      await this.loadEventDates();
      await this.loadRaffleState();
    window.runtime.EventsOn("logUpdate", (message) => {
      this.log = message.split('\n');
      this.scrollBox('logbox');
    });
    window.runtime.EventsOn("debugLogUpdate", (message) => {
      this.debugLog = message.split('\n');
      this.scrollBox('debuglogbox');
    });
    // Python parser debug events removed
    window.runtime.EventsOn("chatLogUpdate", (message) => {
      this.chatLog = message.split('\n');
      this.scrollBox('chatlogbox');
    });

    // Raffle updates from backend
    window.runtime.EventsOn("raffleUpdate", (jsonStr) => {
      try {
        this.loadRaffleState();
      } catch (e) {
        // ignore
      }
    });

    window.runtime.EventsOn("tradeItemsUpdate", (jsonStr) => {
      try {
        this.tradeItems = (JSON.parse(jsonStr) || []).map(item => ({
          ...item,
          displayName: this.formatItemName(item.Name),
        }));
      } catch (_) {
        this.tradeItems = [];
      }
      // Update current trader name from backend when partner items present
      try {
        if (this.tradeItems && this.tradeItems.length > 0) {
          window.go.main.App.GetLastTradePartnerName().then(name => {
            this.currentTraderName = name || '';
          }).catch(() => { this.currentTraderName = ''; });
        } else {
          this.currentTraderName = '';
        }
      } catch (e) {
        this.currentTraderName = '';
      }
    });

    window.runtime.EventsOn("activeGameBetItemsUpdate", (jsonStr) => {
      try {
        this.activeGameBetItems = (JSON.parse(jsonStr) || []).map(item => ({
          ...item,
          displayName: this.formatItemName(item.Name),
        }));
      } catch (_) {
        this.activeGameBetItems = [];
      }
      // Update current game player (who is in-game with dealer)
      try {
        if (this.activeGameBetItems && this.activeGameBetItems.length > 0) {
          window.go.main.App.GetLastTradePartnerName().then(name => {
            this.currentGamePlayerName = name || '';
          }).catch(() => { this.currentGamePlayerName = ''; });
        } else {
          this.currentGamePlayerName = '';
        }
      } catch (e) {
        this.currentGamePlayerName = '';
      }
    });

    // Listen for backend updates to the UO7 payout multiplier
    window.runtime.EventsOn("underOver7PayoutMultiplierChanged", (val) => {
      try {
        const n = Number(val) || 3;
        this.uo7Multiplier = n;
      } catch (e) {
        console.error('underOver7PayoutMultiplierChanged handler error', e);
      }
    });

    window.runtime.EventsOn("ownTradeItemsUpdate", (jsonStr) => {
      try {
        this.ownTradeItems = (JSON.parse(jsonStr) || []).map(item => ({
          ...item,
          displayName: this.formatItemName(item.Name),
        }));
      } catch (_) {
        this.ownTradeItems = [];
      }
    });

    window.runtime.EventsOn("handItemsUpdate", (jsonStr) => {
      try {
        this.handItems = (JSON.parse(jsonStr) || []).map(item => ({
          ...item,
          displayName: this.formatItemName(item.Name),
        }));
      } catch (_) {
        this.handItems = [];
      }
    });

    window.runtime.EventsOn("roomIdentityUpdate", (jsonStr) => {
      try {
        this.roomIdentity = JSON.parse(jsonStr) || [];
      } catch (_) {
        this.roomIdentity = [];
      }
    });
    window.runtime.EventsOn("gameHistoryUpdate", (jsonStr) => {
      try {
        this.gameHistory = JSON.parse(jsonStr) || [];
      } catch (_) {
        this.gameHistory = [];
      }
    });

    window.runtime.EventsOn("casinoStatsUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.casinoStats = parsed;
        this.casinoStatsMap['all_time'] = parsed;
      } catch (_) {
        this.casinoStats = {};
        this.casinoStatsMap['all_time'] = {};
      }
    });

    window.runtime.EventsOn("casinoStatsUpdateToday", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.casinoStatsToday = parsed;
        this.casinoStatsMap['today'] = parsed;
      } catch (_) {
        this.casinoStatsToday = {};
        this.casinoStatsMap['today'] = {};
      }
    });

    // Dice setup updates from backend
    window.runtime.EventsOn("diceSetupUpdate", (jsonStr) => {
      try {
        const payload = JSON.parse(jsonStr || '{}') || {};
        const dice = (payload.dice || []).map(d => ({ id: d.ID, value: d.Value, rolled: d.Value && d.Value > 0 }));
        // ensure correct number of slots based on selected dealer mode
        const expected = (this.onlyUnderOverMode || this.underOver7Mode) ? 2 : 5;
        this.diceSetup = Array.from({ length: expected }).map((_, i) => dice[i] || { id: 0, value: 0, rolled: false });

        const active = !!payload.active;
        const setupActive = !!payload.diceSetupActive;
        const ready = !!payload.ready;

        // If backend reports the casino is not active and setup is not active,
        // immediately switch UI back to stopped so it doesn't remain "Running".
        if (!active && !setupActive) {
          this.casinoStatus = 'Stopped';
          this.casinoStatusKey = 'stopped';
          this.showDiceSetupModal = false;
          const expected = this.onlyUnderOverMode ? 2 : 5;
          this.diceSetup = Array.from({ length: expected }).map(() => ({ id: 0, value: 0, rolled: false }));
          return;
        }

        if (payload.complete) {
          // small delay so user sees final green
          setTimeout(() => {
            this.showDiceSetupModal = false;
            this.addLogMsg('[UI] Dice setup complete');
          }, 700);
          // only flip to running if backend indicates active/ready
          if (active || ready) {
            this.casinoStatus = 'Running';
            this.casinoStatusKey = 'running';
          }
        } else {
          // update awaiting status when setup is active
          if (setupActive) {
            this.casinoStatus = 'Awaiting dice rolls';
            this.casinoStatusKey = 'awaiting';
          }
        }
      } catch (e) {
        console.error('diceSetupUpdate parse', e);
      }
    });
    // Sync UI when backend toggles UnderOver7 dealer mode.
    window.runtime.EventsOn("underOver7ModeChanged", (payload) => {
      try {
        let enabled = payload;
        if (typeof payload === 'string') {
          try { enabled = JSON.parse(payload); } catch (e) {}
        }
        this.underOver7Mode = !!enabled;
        if (this.underOver7Mode) {
          this.riskModeEnabledInput = false;
          try { window.go.main.App.SetRiskEnabled(false); } catch (_) {}
        }
      } catch (e) {
        console.error('underOver7ModeChanged handler', e);
      }
    });
    // AutoShout initial fetch and subscription
    try {
      const cfg = await window.go.main.App.GetAutoShoutConfig();
      if (typeof cfg === 'string') {
        try {
          const parsed = JSON.parse(cfg || '{}') || {};
          this.autoShoutEnabled = !!parsed.enabled;
          this.autoShoutPhrase = parsed.phrase || '';
          this.autoShoutSeconds = parsed.seconds || 30;
        } catch (e) {}
      } else if (cfg) {
        this.autoShoutEnabled = !!cfg.enabled;
        this.autoShoutPhrase = cfg.phrase || '';
        this.autoShoutSeconds = cfg.seconds || 30;
      }
    } catch (e) {
      console.error('autoShout init', e);
    }

    // AutoShout #2 initial fetch
    try {
      const cfgb = await window.go.main.App.GetAutoShoutConfig2();
      if (typeof cfgb === 'string') {
        try {
          const parsed = JSON.parse(cfgb || '{}') || {};
          this.autoShoutEnabled2 = !!parsed.enabled;
          this.autoShoutPhrase2 = parsed.phrase || '';
          this.autoShoutSeconds2 = parsed.seconds || 30;
        } catch (e) {}
      } else if (cfgb) {
        this.autoShoutEnabled2 = !!cfgb.enabled;
        this.autoShoutPhrase2 = cfgb.phrase || '';
        this.autoShoutSeconds2 = cfgb.seconds || 30;
      }
    } catch (e) {
      console.error('autoShout2 init', e);
    }

    // DealerOpen initial fetch
    try {
      const cfg2 = await window.go.main.App.GetDealerOpenConfig();
      if (typeof cfg2 === 'string') {
        try {
          const parsed = JSON.parse(cfg2 || '{}') || {};
          this.dealerOpenEnabled = !!parsed.enabled;
          this.dealerTradeSeconds = parsed.tradeSeconds || parsed.seconds || 45;
          this.dealerAnnounceSeconds = parsed.announceSeconds || parsed.seconds || 45;
        } catch (e) {}
      } else if (cfg2) {
        this.dealerOpenEnabled = !!cfg2.enabled;
        this.dealerTradeSeconds = cfg2.tradeSeconds || cfg2.seconds || 45;
        this.dealerAnnounceSeconds = cfg2.announceSeconds || cfg2.seconds || 45;
      }
    } catch (e) {
      console.error('dealerOpen init', e);
    }

    // Load user-defined auto shout presets from localStorage
    try {
      this.loadAutoShoutPresets();
      this.loadAutoShoutPresets2();
    } catch (e) {}

    // Block recommended-rooms initial fetch and subscription
    try {
      const cfg2 = await window.go.main.App.GetBlockRecommendedRoomsConfig();
      if (typeof cfg2 === 'string') {
        try {
          const parsed = JSON.parse(cfg2 || '{}') || {};
          this.blockRecommendedRooms = !!parsed.enabled;
        } catch (e) {}
      } else if (cfg2) {
        this.blockRecommendedRooms = !!cfg2.enabled;
      }
    } catch (e) {
      console.error('blockRecommended init', e);
    }

    // Block slide-object-bundle (incoming) initial fetch and subscription
    try {
      const cfg3 = await window.go.main.App.GetBlockSlideObjectBundleConfig();
      if (typeof cfg3 === 'string') {
        try {
          const parsed = JSON.parse(cfg3 || '{}') || {};
          this.blockSlideObjectBundle = !!parsed.enabled;
        } catch (e) {}
      } else if (cfg3) {
        this.blockSlideObjectBundle = !!cfg3.enabled;
      }
    } catch (e) {
      console.error('blockSlideObject init', e);
    }

    // Block STATUS_EFFECTS (incoming) initial fetch
    try {
      const cfgS = await window.go.main.App.GetBlockStatusEffectsConfig();
      if (typeof cfgS === 'string') {
        try {
          const parsed = JSON.parse(cfgS || '{}') || {};
          this.blockStatusEffects = !!parsed.enabled;
        } catch (e) {}
      } else if (cfgS) {
        this.blockStatusEffects = !!cfgS.enabled;
      }
    } catch (e) {
      console.error('blockStatusEffects init', e);
    }

    // Block REMOVE_BUDDY (incoming) initial fetch
    try {
      const cfgR = await window.go.main.App.GetBlockRemoveBuddyConfig();
      if (typeof cfgR === 'string') {
        try {
          const parsed = JSON.parse(cfgR || '{}') || {};
          this.blockRemoveBuddy = !!parsed.enabled;
        } catch (e) {}
      } else if (cfgR) {
        this.blockRemoveBuddy = !!cfgR.enabled;
      }
    } catch (e) {
      console.error('blockRemoveBuddy init', e);
    }

    // Block FRIEND_LIST_UPDATE (incoming) initial fetch
    try {
      const cfgF = await window.go.main.App.GetBlockFriendListUpdateConfig();
      if (typeof cfgF === 'string') {
        try {
          const parsed = JSON.parse(cfgF || '{}') || {};
          this.blockFriendListUpdate = !!parsed.enabled;
        } catch (e) {}
      } else if (cfgF) {
        this.blockFriendListUpdate = !!cfgF.enabled;
      }
    } catch (e) {
      console.error('blockFriendListUpdate init', e);
    }

    // New incoming: ARTICLES_PAGE (681) initial fetch
    try {
      const aCfg = await window.go.main.App.GetBlockArticlesPageConfig();
      if (typeof aCfg === 'string') {
        try {
          const parsed = JSON.parse(aCfg || '{}') || {};
          this.blockArticlesPage = !!parsed.enabled;
        } catch (e) {}
      } else if (aCfg) {
        this.blockArticlesPage = !!aCfg.enabled;
      }
    } catch (e) {
      console.error('blockArticlesPage init', e);
    }

    // New incoming: FAVOURITEROOMRESULTS (61) initial fetch
    try {
      const fCfg = await window.go.main.App.GetBlockFavouriteRoomResultsConfig();
      if (typeof fCfg === 'string') {
        try {
          const parsed = JSON.parse(fCfg || '{}') || {};
          this.blockFavouriteRoomResults = !!parsed.enabled;
        } catch (e) {}
      } else if (fCfg) {
        this.blockFavouriteRoomResults = !!fCfg.enabled;
      }
    } catch (e) {
      console.error('blockFavouriteRoomResults init', e);
    }

    // New incoming: CALENDAR_EVENTS (683) initial fetch
    try {
      const cCfg = await window.go.main.App.GetBlockCalendarEventsConfig();
      if (typeof cCfg === 'string') {
        try {
          const parsed = JSON.parse(cCfg || '{}') || {};
          this.blockCalendarEvents = !!parsed.enabled;
        } catch (e) {}
      } else if (cCfg) {
        this.blockCalendarEvents = !!cCfg.enabled;
      }
    } catch (e) {
      console.error('blockCalendarEvents init', e);
    }

    // New incoming: USER_BANNED (35) initial fetch
    try {
      const ubCfg = await window.go.main.App.GetBlockUserBannedConfig();
      if (typeof ubCfg === 'string') {
        try {
          const parsed = JSON.parse(ubCfg || '{}') || {};
          this.blockUserBanned = !!parsed.enabled;
        } catch (e) {}
      } else if (ubCfg) {
        this.blockUserBanned = !!ubCfg.enabled;
      }
    } catch (e) {
      console.error('blockUserBanned init', e);
    }

    // New incoming: 4095 (raw) initial fetch
    try {
      const rCfg = await window.go.main.App.GetBlockIncoming4095Config();
      if (typeof rCfg === 'string') {
        try {
          const parsed = JSON.parse(rCfg || '{}') || {};
          this.blockIncoming4095 = !!parsed.enabled;
        } catch (e) {}
      } else if (rCfg) {
        this.blockIncoming4095 = !!rCfg.enabled;
      }
    } catch (e) {
      console.error('blockIncoming4095 init', e);
    }

    // Outgoing initial fetches
    try {
      const outRec = await window.go.main.App.GetBlockRecommendedRoomsOutgoingConfig();
      if (typeof outRec === 'string') {
        try {
          const parsed = JSON.parse(outRec || '{}') || {};
          this.blockRecommendedRoomsOutgoing = !!parsed.enabled;
        } catch (e) {}
      } else if (outRec) {
        this.blockRecommendedRoomsOutgoing = !!outRec.enabled;
      }
    } catch (e) {
      console.error('blockRecommendedOutgoing init', e);
    }

    try {
      const outSlide = await window.go.main.App.GetBlockSlideObjectBundleOutgoingConfig();
      if (typeof outSlide === 'string') {
        try {
          const parsed = JSON.parse(outSlide || '{}') || {};
          this.blockSlideObjectBundleOutgoing = !!parsed.enabled;
        } catch (e) {}
      } else if (outSlide) {
        this.blockSlideObjectBundleOutgoing = !!outSlide.enabled;
      }
    } catch (e) {
      console.error('blockSlideObjectOutgoing init', e);
    }

    // Outgoing poll-event-eligibility initial fetch
    try {
      const outPoll = await window.go.main.App.GetBlockPollEventEligibilityOutgoingConfig();
      if (typeof outPoll === 'string') {
        try {
          const parsed = JSON.parse(outPoll || '{}') || {};
          this.blockPollEventEligibilityOutgoing = !!parsed.enabled;
        } catch (e) {}
      } else if (outPoll) {
        this.blockPollEventEligibilityOutgoing = !!outPoll.enabled;
      }
    } catch (e) {
      console.error('blockPollEventEligibilityOutgoing init', e);
    }

    // New outgoing: GET_PAGE_ARTICLES (680) initial fetch
    try {
      const outA = await window.go.main.App.GetBlockGetPageArticlesOutgoingConfig();
      if (typeof outA === 'string') {
        try {
          const parsed = JSON.parse(outA || '{}') || {};
          this.blockGetPageArticlesOutgoing = !!parsed.enabled;
        } catch (e) {}
      } else if (outA) {
        this.blockGetPageArticlesOutgoing = !!outA.enabled;
      }
    } catch (e) {
      console.error('blockGetPageArticlesOutgoing init', e);
    }

    // New outgoing: GET_CALENDAR_EVENTS (682) initial fetch
    try {
      const outC = await window.go.main.App.GetBlockGetCalendarEventsOutgoingConfig();
      if (typeof outC === 'string') {
        try {
          const parsed = JSON.parse(outC || '{}') || {};
          this.blockGetCalendarEventsOutgoing = !!parsed.enabled;
        } catch (e) {}
      } else if (outC) {
        this.blockGetCalendarEventsOutgoing = !!outC.enabled;
      }
    } catch (e) {
      console.error('blockGetCalendarEventsOutgoing init', e);
    }

    // New outgoing: FRIENDLIST_UPDATE (15) initial fetch
    try {
      const outF = await window.go.main.App.GetBlockFriendListUpdateOutgoingConfig();
      if (typeof outF === 'string') {
        try {
          const parsed = JSON.parse(outF || '{}') || {};
          this.blockFriendListUpdateOutgoing = !!parsed.enabled;
        } catch (e) {}
      } else if (outF) {
        this.blockFriendListUpdateOutgoing = !!outF.enabled;
      }
    } catch (e) {
      console.error('blockFriendListUpdateOutgoing init', e);
    }

    window.runtime.EventsOn("blockSlideObjectBundleUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockSlideObjectBundle = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockStatusEffectsUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockStatusEffects = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockArticlesPageUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockArticlesPage = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockFavouriteRoomResultsUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockFavouriteRoomResults = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockCalendarEventsUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockCalendarEvents = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockUserBannedUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockUserBanned = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockIncoming4095Update", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockIncoming4095 = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockRemoveBuddyUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockRemoveBuddy = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockFriendListUpdateUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockFriendListUpdate = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockPollEventEligibilityOutgoingUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockPollEventEligibilityOutgoing = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockRecommendedUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockRecommendedRooms = !!parsed.enabled;
      } catch (e) {}
    });

    // Outgoing update subscriptions
    window.runtime.EventsOn("blockRecommendedOutgoingUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockRecommendedRoomsOutgoing = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockGetPageArticlesOutgoingUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockGetPageArticlesOutgoing = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockGetCalendarEventsOutgoingUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockGetCalendarEventsOutgoing = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockFriendListUpdateOutgoingUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockFriendListUpdateOutgoing = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("blockSlideObjectBundleOutgoingUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.blockSlideObjectBundleOutgoing = !!parsed.enabled;
      } catch (e) {}
    });

    window.runtime.EventsOn("autoShoutUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.autoShoutEnabled = !!parsed.enabled;
        this.autoShoutPhrase = parsed.phrase || '';
        this.autoShoutSeconds = parsed.seconds || 30;
      } catch (e) {}
    });
    window.runtime.EventsOn("autoShoutUpdate2", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.autoShoutEnabled2 = !!parsed.enabled;
        this.autoShoutPhrase2 = parsed.phrase || '';
        this.autoShoutSeconds2 = parsed.seconds || 30;
      } catch (e) {}
    });
    window.runtime.EventsOn("dealerOpenUpdate", (jsonStr) => {
      try {
        const parsed = JSON.parse(jsonStr || '{}') || {};
        this.dealerOpenEnabled = !!parsed.enabled;
        this.dealerTradeSeconds = parsed.tradeSeconds || parsed.seconds || 45;
        this.dealerAnnounceSeconds = parsed.announceSeconds || parsed.seconds || 45;
      } catch (e) {}
    });
  },
  beforeUnmount() {}
};
</script>


<style scoped>
body {
  background-color: #100e0e!important;
}
.poker-config-section {
  padding: 20px;
  background-color: #100e0e;
  border-radius: 8px;
  color: #fff;
}

.section-title {
  font-size: 18px;
  margin-bottom: 10px;
  color: #e0e0e0;
  text-align: center;
}

.form-group {
  display: flex;
  justify-content: center;
  align-items: center;
  margin-bottom: 10px;
}

label {
  flex: 1;
  font-weight: bold;
  text-align: right;
  margin-right: 10px;
  font-size: 14px;
  color: #c0c0c0;
}

input[type="text"] {
  flex: 2;
  padding: 8px;
  background-color: #2e2e2e;
  border: 1px solid #444;
  border-radius: 4px;
  color: #fff;
  text-transform: none;
  font-size: 14px;
  max-width: 300px;
}

input[type="text"]::placeholder {
  color: #888;
}

.save-button {
  display: block;
  width: 100%;
  padding: 8px;
  background-color: #2f2f2f;
  color: white;
  border: solid .2px #444;
  border-radius: 4px;
  cursor: pointer;
  margin-top: 15px;
  font-size: 14px;
}

.save-button:hover {
  background-color: #1e1e1e;
}

.config-intro {
  margin: 0 0 16px;
  color: #b8b8b8;
  font-size: 14px;
  line-height: 1.6;
  text-align: center;
}

.game-card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
  margin-bottom: 18px;
}

.game-card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
  padding: 14px;
  background: #181818;
  border: 1px solid #3a3a3a;
  border-radius: 8px;
  color: #e8e8e8;
  text-align: left;
  cursor: pointer;
}

.game-card:hover {
  background: #202020;
  border-color: #5a5a5a;
}

.game-card-title {
  font-size: 15px;
  font-weight: 700;
}

.game-card-summary {
  color: #a6a6a6;
  font-size: 13px;
  line-height: 1.5;
}

.game-card-action {
  color: #ffd700;
  font-size: 12px;
}

.game-guide-modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  z-index: 999;
}

.game-guide-modal {
  width: min(680px, 100%);
  background: #141414;
  border: 1px solid #444;
  border-radius: 10px;
  padding: 18px;
}

/* Dealer name modal overrides to ensure it fits small windows */
.dealer-name-modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 12px;
  z-index: 1000;
  overflow: auto;
}

.dealer-name-modal {
  width: min(560px, 100%);
  max-width: 96vw;
  max-height: calc(100vh - 48px);
  background: #141414;
  border: 1px solid #444;
  border-radius: 10px;
  padding: 16px;
  box-sizing: border-box;
  overflow-y: auto;
}

/* Ensure general modals don't overflow viewport */
.game-guide-modal, .dice-setup-modal {
  max-height: calc(100vh - 48px);
  box-sizing: border-box;
  overflow-y: auto;
}

.dice-setup-modal {
  width: min(420px, 100%);
  background: #141414;
  border: 1px solid #444;
  border-radius: 10px;
  padding: 18px;
  text-align: center;
}

.dice-circles {
  display: flex;
  gap: 12px;
  justify-content: center;
  margin-top: 12px;
}

.dice-circle {
  width: 42px;
  height: 42px;
  border-radius: 50%;
  background: #3a3a3a;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  font-weight: 700;
}

.dice-circle.rolled {
  background: #2ecc71;
}

.casino-panel {
  margin: 18px 0;
  padding: 12px;
  background: #141414;
  border: 1px solid #2f2f2f;
  border-radius: 8px;
}

.casino-panel-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.casino-info {
  display: flex;
  align-items: center;
  gap: 12px;
}

.casino-status-label {
  color: #cfcfcf;
  font-weight: 700;
}

.casino-status {
  padding: 6px 10px;
  border-radius: 6px;
  font-weight: 700;
}

.casino-status.stopped {
  background: #e74c3c;
  color: #fff;
}

.casino-status.paused {
  background: #f39c12;
  color: #111;
}

.casino-status.running {
  background: #2ecc71;
  color: #081007;
}

.casino-status.awaiting {
  background: #f1c40f;
  color: #111;
}

/* Packet info tooltip */
.packet-info {
  position: relative;
  display: inline-block;
  width: 18px;
  height: 18px;
  font-size: 12px;
  color: #9ec5ff;
  background: rgba(255,255,255,0.02);
  border-radius: 50%;
  text-align: center;
  line-height: 18px;
  cursor: default;
}
.packet-info .tooltip {
  display: none;
  position: absolute;
  bottom: calc(100% + 8px);
  left: 50%;
  transform: translateX(-50%);
  background: #111;
  color: #fff;
  padding: 8px;
  border-radius: 6px;
  width: 260px;
  font-size: 12px;
  box-shadow: 0 6px 18px rgba(0,0,0,0.5);
  z-index: 9999;
  text-align: left;
}
.packet-info .tooltip::after {
  content: '';
  position: absolute;
  top: 100%;
  left: 50%;
  transform: translateX(-50%);
  border-width: 6px;
  border-style: solid;
  border-color: #111 transparent transparent transparent;
}

/* Fixed tooltip element rendered at root and positioned via JS */
.packet-tooltip {
  position: fixed;
  z-index: 2147483647;
  background: #111;
  color: #fff;
  padding: 8px;
  border-radius: 6px;
  width: 320px;
  font-size: 12px;
  box-shadow: 0 8px 26px rgba(0,0,0,0.6);
  pointer-events: none;
  text-align: left;
}

.packet-tooltip::after {
  content: '';
  position: absolute;
  left: 50%;
  transform: translateX(-50%);
  border-width: 6px;
  border-style: solid;
  border-color: #111 transparent transparent transparent;
  top: 100%;
}

.packet-tooltip .tooltip-header {
  font-weight: 700;
  margin-bottom: 6px;
  color: #ffd700;
  font-size: 13px;
}
.packet-tooltip .tooltip-body {
  color: #e6e6e6;
  font-size: 12px;
  line-height: 1.4;
}

.game-guide-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}

.game-guide-title {
  margin: 0;
  text-align: left;
}

.game-guide-block {
  margin-top: 14px;
  padding-top: 14px;
  border-top: 1px solid #2f2f2f;
}

.game-guide-label {
  color: #ffd700;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
  margin-bottom: 6px;
}

.game-guide-text {
  color: #d0d0d0;
  font-size: 14px;
  line-height: 1.6;
}

.dealer-limits-grid {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}
.dealer-limit-field {
  flex: 1 1 0;
}
.modal-number {
  width: 160px;
  padding: 8px;
  border-radius: 4px;
  border: 1px solid #444;
  background-color: #2e2e2e;
  color: #fff;
}
.trade-limits-home {
  margin-top: 8px;
  font-size: 13px;
  color: #c0c0c0;
  text-align: center;
}

.history-search {
  width: 100%;
  padding: 10px 12px;
  background-color: #1a1a1a;
  border: 1px solid #3f3f3f;
  border-radius: 8px;
  color: #f0f0f0;
  margin-bottom: 14px;
  box-sizing: border-box;
}

.history-search-wrapper {
  position: relative;
}
.history-suggestions {
  position: absolute;
  left: 0;
  right: 0;
  top: calc(100% + 6px);
  background: #151515;
  border: 1px solid #333;
  border-radius: 6px;
  max-height: 220px;
  overflow-y: auto;
  z-index: 200;
  list-style: none;
  margin: 0;
  padding: 6px 0;
}
.history-suggestions li {
  padding: 8px 12px;
  cursor: pointer;
  color: #e0e0e0;
}
.history-suggestions li:hover {
  background: #222;
}
.history-filter-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.history-filter-flagged {
  color: #e0e0e0;
  font-size: 13px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.history-filter-flagged input[type="checkbox"] {
  width: 16px;
  height: 16px;
}

.history-actions {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}

.history-list {
  display: grid;
  gap: 12px;
}

.history-card {
  padding: 14px;
  background: #161616;
  border: 1px solid #343434;
  border-radius: 8px;
  text-align: left;
  color: #efefef;
  cursor: pointer;
}

.history-card-issue {
  border-color: #a94442;
  background: #1d1414;
}

.history-danger-btn {
  background: #4a1f1f;
  border-color: #a94442;
  color: #ffd0d0;
}

.history-danger-btn:hover {
  background: #5a2323;
}

.history-card-top,
.history-meta-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}

.history-player {
  font-size: 15px;
  font-weight: 700;
}

.history-status {
  padding: 3px 8px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
}

.history-status-complete {
  background: #16351f;
  color: #8ef0aa;
}

.history-status-pending {
  background: #3f3113;
  color: #ffd36d;
}

.history-status-info {
  background: #1b3042;
  color: #8fc8ff;
}

.history-status-issue {
  background: #4a1f1f;
  color: #ff9e9e;
}

.history-summary,
.history-issue-text {
  color: #bdbdbd;
  font-size: 13px;
  line-height: 1.5;
}

.history-issue-text {
  color: #ff9e9e;
}

.history-modal {
  max-height: 85vh;
  overflow-y: auto;
}

.history-detail-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
}

.history-detail-item {
  padding: 10px;
  background: #191919;
  border: 1px solid #2b2b2b;
  border-radius: 8px;
}

.history-notes {
  display: grid;
  gap: 8px;
}

.confirm-modal {
  width: min(520px, 100%);
}

.confirm-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 18px;
}

.log-section {
  background-color: #000000; /* Black background for the console */
  padding: 10px;
  border-radius: 4px;
  height: 200px;
  overflow-y: auto;
  overflow-x: hidden;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  word-wrap: break-word;
  word-break: break-word;
  box-sizing: border-box;
  max-width: 100%;
  font-family: monospace;
  margin-top: 10px;
  color: #00ff00; /* Green text color for the hacker console style */
  font-size: 13px;
  line-height: 1.4em;
  border: 2px solid #00ff00; /* Optional: Green border for console look */
}

/* Add some padding to each log message for readability */
.log-section div {
  padding: 2px 0;
}

.log-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 16px;
}

.log-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.copy-btn {
  padding: 6px 10px;
  background-color: #1f1f1f;
  color: #d8d8d8;
  border: 1px solid #4a4a4a;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
}

.copy-btn:hover {
  background-color: #2a2a2a;
}

.chat-log-section {
  color: #7dd3fc;
  border-color: #7dd3fc;
}

.debug-log-section {
  color: #fda4af;
  border-color: #fda4af;
}

/* Tab navigation */
.tab-bar {
  display: flex;
  gap: 4px;
  margin-bottom: 16px;
}
.tab-btn {
  flex: 1;
  padding: 8px;
  background-color: #2f2f2f;
  color: #c0c0c0;
  border: 1px solid #444;
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
}
.tab-btn:hover {
  background-color: #1e1e1e;
}
.tab-btn.active {
  background-color: #444;
  color: #fff;
  border-color: #888;
}

/* Catalog table */
.catalog-table {
  width: 100%;
  border-collapse: collapse;
  margin-bottom: 10px;
  font-size: 13px;
}
.catalog-table th {
  text-align: left;
  padding: 6px 8px;
  color: #c0c0c0;
  border-bottom: 1px solid #444;
}
.catalog-table td {
  padding: 4px 4px;
}
.catalog-input {
  width: 100%;
  padding: 5px 6px;
  background-color: #2e2e2e;
  border: 1px solid #444;
  border-radius: 4px;
  color: #fff;
  font-size: 13px;
  box-sizing: border-box;
}
.catalog-value {
  width: 80px;
}
.catalog-label {
  display: block;
  padding: 5px 6px;
  color: #aaa;
  font-size: 13px;
}

.trade-divider {
  border: none;
  border-top: 1px solid #333;
  margin: 18px 0;
}

.trade-empty {
  text-align: center;
  color: #888;
  padding: 24px 12px;
  font-size: 14px;
  line-height: 1.8;
}
.trade-total-label {
  text-align: right;
  padding: 8px 8px;
  color: #e0e0e0;
  font-weight: bold;
  font-size: 14px;
  border-top: 1px solid #444;
}
.trade-total-value {
  padding: 8px 8px;
  color: #ffd700;
  font-weight: bold;
  font-size: 14px;
  border-top: 1px solid #444;
}
.unlisted-value {
  color: #ff8888;
}
.trade-hint {
  font-size: 12px;
  color: #888;
  margin-top: 10px;
  text-align: center;
}

/* Stats styles */
.stats-range-controls {
  display: flex;
  gap: 8px;
  justify-content: center;
  margin-bottom: 12px;
}
.stats-summary-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}
.stat-card {
  background: linear-gradient(180deg, #171717, #141414);
  border: 1px solid #2f2f2f;
  padding: 12px;
  border-radius: 8px;
  text-align: left;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.stat-icon {
  font-size: 18px;
}
.stat-label {
  color: #cfcfcf;
  font-weight: 700;
  font-size: 12px;
}
.stat-value {
  font-size: 20px;
  font-weight: 800;
  color: #fff;
  display: flex;
  align-items: center;
  gap: 8px;
}
.stat-value.positive { color: #2ecc71; }
.stat-value.negative { color: #e74c3c; }
.stat-num { font-weight: 800; }
.stat-value-small {
  font-size: 16px;
  line-height: 1.3;
}
.stats-performance-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.stats-game-grid { grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); }
.stats-game-card { position: relative; }
.net-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border-radius: 14px;
  font-weight: 700;
  margin-top: 8px;
}
.net-badge.positive { background: rgba(46,204,113,0.08); color: #2ecc71; border: 1px solid rgba(46,204,113,0.15); }
.net-badge.negative { background: rgba(231,76,60,0.06); color: #e74c3c; border: 1px solid rgba(231,76,60,0.12); }

.stats-table td.positive { color: #2ecc71; font-weight: 700; }
.stats-table td.negative { color: #e74c3c; font-weight: 700; }

.game-settings-box {
  border: 1px solid #333;
  border-radius: 6px;
  padding: 12px;
  background: #151515;
}

.game-settings-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}

.game-settings-row:last-of-type {
  margin-bottom: 0;
}

.game-settings-label {
  flex: 1;
  text-align: left;
  margin-right: 0;
}

.game-settings-input {
  flex: 1;
  max-width: 180px;
  padding: 8px;
  background-color: #2e2e2e;
  border: 1px solid #444;
  border-radius: 4px;
  color: #fff;
  font-size: 14px;
}

.game-settings-value {
  flex: 1;
  max-width: 180px;
  padding: 8px;
  border: 1px solid #333;
  border-radius: 4px;
  background-color: #1f1f1f;
  color: #ffd700;
  font-weight: 700;
}

.game-settings-save {
  margin-top: 12px;
}
/* Monospace pre blocks that wrap long lines to avoid horizontal scroll */
.monospace-wrap {
  max-height: 300px;
  background: #111;
  color: #dcdcdc;
  padding: 8px;
  border-radius: 6px;
  font-family: monospace;
  white-space: pre-wrap; /* wrap long lines */
  overflow-wrap: anywhere;
  word-wrap: break-word;
  word-break: break-word;
  overflow-x: hidden; /* prevent horizontal scroll */
  overflow-y: auto;
  width: 100%;
  box-sizing: border-box;
}
.monospace-wrap.small { max-height: 120px; }

/* Users bottom bar */
.users-bottom-bar {
  position: fixed;
  left: 20px;
  right: 20px;
  bottom: 18px;
  background: rgba(20,20,20,0.95);
  border: 1px solid #2f2f2f;
  border-radius: 10px;
  padding: 10px 12px;
  z-index: 900;
  box-shadow: 0 6px 18px rgba(0,0,0,0.6);
  display: flex;
  justify-content: center;
}
.users-bottom-inner {
  width: 100%;
  max-width: 1200px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.users-list {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: center;
}
.user-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  background: #151515;
  border: 1px solid #2b2b2b;
  color: #e8e8e8;
  border-radius: 18px;
  font-size: 13px;
  min-width: 88px;
  justify-content: center;
}
.user-name {
  font-weight: 700;
  color: #fff;
}
.user-badge {
  font-size: 11px;
  padding: 3px 8px;
  border-radius: 12px;
  color: #111;
}
.trading-badge {
  background: #ffd36d;
}
.game-badge {
  background: #8ef0aa;
}
.user-pill.trading {
  border-color: #d9b363;
}
.user-pill.in-game {
  border-color: #4bd07c;
}
.users-empty {
  color: #bdbdbd;
  font-size: 13px;
}
/* Auto shout preset button styles */
.preset-btn {
  padding: 6px 10px;
  background-color: #2f2f2f;
  color: #fff;
  border: 1px solid #444;
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}
.delete-preset {
  padding: 6px 8px;
  background: #3a1f1f;
  color: #ffd0d0;
  border: 1px solid #5a2b2b;
  border-radius: 6px;
  cursor: pointer;
  font-weight: 700;
}
.delete-preset:hover {
  background: #5a2323;
}
</style>