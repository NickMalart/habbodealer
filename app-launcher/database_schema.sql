-- Canonical SQLite schema for every workspace app that uses the shared database.
-- Keep this in sync with the app-level initializers. CREATE IF NOT EXISTS makes
-- ResetDatabase/CreateMissingTables safe to run repeatedly.

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Root dealer application
CREATE TABLE IF NOT EXISTS game_history_entries (
    id TEXT NOT NULL,
    owner_key TEXT NOT NULL DEFAULT '',
    player_name TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT '',
    completed_at TEXT NOT NULL DEFAULT '',
    game TEXT NOT NULL DEFAULT '',
    winner TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    issue BOOLEAN NOT NULL DEFAULT FALSE,
    issue_reason TEXT NOT NULL DEFAULT '',
    player_result TEXT NOT NULL DEFAULT '',
    dealer_result TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '[]',
    choice TEXT NOT NULL DEFAULT '',
    choice_shout TEXT NOT NULL DEFAULT '',
    payout_multiplier INTEGER NOT NULL DEFAULT 0,
    raffle_session_id BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_db_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    rolls TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (id, owner_key)
);
CREATE TABLE IF NOT EXISTS game_history_items (
    entry_id TEXT NOT NULL,
    owner_key TEXT NOT NULL DEFAULT '',
    item_type TEXT NOT NULL,
    item_index INTEGER NOT NULL,
    item_name TEXT NOT NULL DEFAULT '',
    quantity INTEGER NOT NULL DEFAULT 1,
    raw_data TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (entry_id, owner_key, item_type, item_index),
    FOREIGN KEY (entry_id, owner_key)
        REFERENCES game_history_entries(id, owner_key)
        ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_game_history_owner_started
    ON game_history_entries(owner_key, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_game_history_items_owner_entry
    ON game_history_items(owner_key, entry_id, item_type, item_index);
CREATE TABLE IF NOT EXISTS trade_ledger (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_key TEXT NOT NULL,
    partner_name TEXT NOT NULL,
    trade_type TEXT NOT NULL,
    total_quantity INTEGER NOT NULL,
    items TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_trade_ledger_owner_created
    ON trade_ledger(owner_key, created_at DESC);
CREATE TABLE IF NOT EXISTS dealer_shouts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_key TEXT NOT NULL DEFAULT '',
    target_player TEXT NOT NULL,
    message TEXT NOT NULL,
    shout_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME NULL
);
CREATE INDEX IF NOT EXISTS idx_dealer_shouts_status_owner
    ON dealer_shouts(status, owner_key);
CREATE TABLE IF NOT EXISTS stocked_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_key TEXT NOT NULL DEFAULT '',
    raw_name TEXT NOT NULL,
    canonical_name TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS stocked_items_owner_raw_name_idx
    ON stocked_items(owner_key, raw_name);

-- Auto Payout Bot and banker inventory shared by dealer/payout workflows
CREATE TABLE IF NOT EXISTS auto_payouts (
    id TEXT PRIMARY KEY,
    player_name TEXT NOT NULL,
    item_name TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    player_trade_id INTEGER NULL,
    banker_trade_id INTEGER NULL,
    notified BOOLEAN DEFAULT FALSE
);
CREATE TABLE IF NOT EXISTS banker_trades (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    player_name TEXT NOT NULL DEFAULT '',
    bet_items TEXT NOT NULL DEFAULT '[]',
    banker_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    player_trade_id INTEGER NULL,
    player_chat_id INTEGER NULL,
    owner_key TEXT NOT NULL DEFAULT '',
    bet_amount INTEGER DEFAULT 0,
    risk_bank INTEGER DEFAULT 0,
    risk_status TEXT DEFAULT 'idle'
);
CREATE TABLE IF NOT EXISTS banker_inventory (
    banker_name TEXT NOT NULL DEFAULT '',
    item_name TEXT NOT NULL DEFAULT '',
    quantity INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (banker_name, item_name)
);
CREATE TABLE IF NOT EXISTS banned_players (
    ban_key TEXT PRIMARY KEY,
    expires_at DATETIME NOT NULL,
    message TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS auto_payout_settings (
    setting_key TEXT PRIMARY KEY,
    setting_value TEXT NOT NULL
);

-- Casino Statistics
CREATE TABLE IF NOT EXISTS blocked_players (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_key TEXT NOT NULL,
    player_name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (owner_key, player_name)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_blocked_players_lower_name
    ON blocked_players(LOWER(player_name));
CREATE TABLE IF NOT EXISTS ui_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Free Raffle Bot
CREATE TABLE IF NOT EXISTS raffle_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at DATETIME NOT NULL,
    scheduled_end_at DATETIME NULL,
    ended_at DATETIME NULL,
    owner_key TEXT NOT NULL DEFAULT '',
    bonus_every INTEGER NOT NULL DEFAULT 5,
    last_seen_created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_entry_id TEXT NOT NULL DEFAULT '',
    last_seen_banker_at DATETIME NOT NULL DEFAULT '2000-01-01',
    last_seen_banker_id BIGINT NOT NULL DEFAULT 0,
    webhook_message_id TEXT NOT NULL DEFAULT '',
    raffle_name TEXT NOT NULL DEFAULT 'Flame Raffle',
    prize_name TEXT NOT NULL DEFAULT 'Purple Dragon Lamp',
    prize_qty INTEGER NOT NULL DEFAULT 1,
    hero_image_url TEXT NOT NULL DEFAULT '',
    hero_attachment_id TEXT NOT NULL DEFAULT '',
    hero_attachment_file TEXT NOT NULL DEFAULT '',
    winner_name TEXT NOT NULL DEFAULT '',
    winner_tickets INTEGER NOT NULL DEFAULT 0,
    winner_odds TEXT NOT NULL DEFAULT '',
    winner_drawn_at DATETIME NULL,
    winner_method TEXT NOT NULL DEFAULT '',
    winner_summary TEXT NOT NULL DEFAULT '',
    winner_proof_url TEXT NOT NULL DEFAULT '',
    winner_proof_id TEXT NOT NULL DEFAULT '',
    winner_proof_file TEXT NOT NULL DEFAULT '',
    sponsor_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    sponsor_name TEXT NOT NULL DEFAULT '',
    sponsor_room_name TEXT NOT NULL DEFAULT '',
    sponsor_image_url TEXT NOT NULL DEFAULT '',
    sponsor_attachment_id TEXT NOT NULL DEFAULT '',
    sponsor_attachment_file TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS raffle_participants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id BIGINT NOT NULL REFERENCES raffle_sessions(id) ON DELETE CASCADE,
    owner_key TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL,
    username_key TEXT NOT NULL DEFAULT '',
    bet_count INTEGER NOT NULL DEFAULT 1,
    ticket_count INTEGER NOT NULL DEFAULT 1,
    manual_ticket_delta INTEGER NOT NULL DEFAULT 0,
    first_bet_at DATETIME NOT NULL,
    last_bet_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (session_id, owner_key, username_key)
);
CREATE INDEX IF NOT EXISTS idx_raffle_sessions_owner ON raffle_sessions(owner_key);
CREATE INDEX IF NOT EXISTS idx_raffle_sessions_started ON raffle_sessions(started_at);
CREATE INDEX IF NOT EXISTS idx_raffle_participants_session ON raffle_participants(session_id, owner_key);
CREATE INDEX IF NOT EXISTS idx_raffle_participants_tickets ON raffle_participants(session_id, ticket_count DESC);

-- Trade Tracker
CREATE TABLE IF NOT EXISTS trade_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at DATETIME NOT NULL,
    ended_at DATETIME NULL,
    owner_key TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS trade_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id BIGINT NOT NULL REFERENCES trade_sessions(id) ON DELETE CASCADE,
    occurred_at DATETIME NOT NULL,
    partner_name TEXT NOT NULL,
    partner_trade_id INTEGER NOT NULL,
    payload_hex TEXT NOT NULL,
    furni_items TEXT DEFAULT '[]',
    owner_key TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS trade_entry_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    trade_entry_id BIGINT NOT NULL REFERENCES trade_entries(id) ON DELETE CASCADE,
    item_name TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    raw_data TEXT NOT NULL DEFAULT '',
    owner_key TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (trade_entry_id, item_name)
);
CREATE INDEX IF NOT EXISTS idx_trade_entries_session_id ON trade_entries(session_id);
CREATE INDEX IF NOT EXISTS idx_trade_entries_occurred_at ON trade_entries(occurred_at);
CREATE INDEX IF NOT EXISTS idx_trade_sessions_owner_key ON trade_sessions(owner_key);
CREATE INDEX IF NOT EXISTS idx_trade_entries_owner_key ON trade_entries(owner_key);
CREATE INDEX IF NOT EXISTS idx_trade_entry_items_entry_id ON trade_entry_items(trade_entry_id);
CREATE INDEX IF NOT EXISTS idx_trade_entry_items_owner_key ON trade_entry_items(owner_key);
CREATE INDEX IF NOT EXISTS idx_trade_entry_items_name ON trade_entry_items(item_name);

-- Multipurpose App
CREATE TABLE IF NOT EXISTS room_rights (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    added_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO schema_migrations (version) VALUES (1);
