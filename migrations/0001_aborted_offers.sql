-- Migration: create aborted trade offers tables
BEGIN;

CREATE TABLE IF NOT EXISTS aborted_trade_offers (
  id BIGSERIAL PRIMARY KEY,
  session_id BIGINT NULL,
  occurred_at TIMESTAMPTZ NOT NULL,
  partner_name TEXT NOT NULL DEFAULT '',
  partner_trade_id INTEGER NOT NULL DEFAULT 0,
  payload_hex TEXT NOT NULL DEFAULT '',
  furni_items JSONB DEFAULT '[]'::jsonb,
  owner_key TEXT NOT NULL DEFAULT '',
  closed_reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS aborted_trade_offer_items (
  id BIGSERIAL PRIMARY KEY,
  aborted_offer_id BIGINT NOT NULL REFERENCES aborted_trade_offers(id) ON DELETE CASCADE,
  item_name TEXT NOT NULL,
  quantity INTEGER NOT NULL DEFAULT 1,
  raw_data TEXT NOT NULL DEFAULT '',
  owner_key TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (aborted_offer_id, item_name)
);

CREATE INDEX IF NOT EXISTS idx_aborted_offers_owner ON aborted_trade_offers(owner_key);
CREATE INDEX IF NOT EXISTS idx_aborted_offers_occurred ON aborted_trade_offers(occurred_at);
CREATE INDEX IF NOT EXISTS idx_aborted_items_name ON aborted_trade_offer_items(item_name);

COMMIT;
