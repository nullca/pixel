-- Midway Quest: server-authoritative economy
BEGIN;

CREATE TABLE IF NOT EXISTS players (
  id          TEXT PRIMARY KEY,                -- KID "sub"
  name        TEXT NOT NULL,
  dept        TEXT NOT NULL,
  is_admin    BOOLEAN NOT NULL DEFAULT FALSE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wallets (
  player_id   TEXT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
  coins       BIGINT NOT NULL DEFAULT 0,
  kp          BIGINT NOT NULL DEFAULT 0,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT wallet_non_negative CHECK (coins >= 0 AND kp >= 0)
);

-- Append-only. Never UPDATE or DELETE rows here.
CREATE TABLE IF NOT EXISTS ledger (
  id          BIGSERIAL PRIMARY KEY,
  player_id   TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  currency    TEXT NOT NULL CHECK (currency IN ('coin','kp')),
  delta       BIGINT NOT NULL,
  balance     BIGINT NOT NULL,                  -- balance after this entry
  reason      TEXT NOT NULL,                    -- e.g. minigame:run, quiz:correct, shop:buy, admin:grant
  ref         JSONB NOT NULL DEFAULT '{}',
  idem_key    TEXT NOT NULL,
  actor       TEXT,                             -- admin id for manual grants
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (player_id, idem_key, currency)
);
CREATE INDEX IF NOT EXISTS ledger_player_time ON ledger (player_id, created_at DESC);
CREATE INDEX IF NOT EXISTS ledger_reason_time ON ledger (reason, created_at DESC);

-- Idempotent responses: the first result of an action is stored and replayed for duplicates.
CREATE TABLE IF NOT EXISTS action_results (
  player_id   TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  idem_key    TEXT NOT NULL,
  response    JSONB NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (player_id, idem_key)
);

CREATE TABLE IF NOT EXISTS daily_counters (
  player_id   TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  day         DATE NOT NULL,                    -- Asia/Bangkok calendar day
  key         TEXT NOT NULL,                    -- run, kite, balloon, fish, sold:<item>, quiz_reward ...
  count       INT  NOT NULL DEFAULT 0,
  PRIMARY KEY (player_id, day, key)
);

CREATE TABLE IF NOT EXISTS items (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  kind        TEXT NOT NULL,                    -- fish, food, material, deco, wear, title, trash
  buy_price   INT,                              -- NULL = not sold in shops
  buy_currency TEXT NOT NULL DEFAULT 'coin' CHECK (buy_currency IN ('coin','kp')),
  sell_price  INT NOT NULL DEFAULT 0,           -- 0 = cannot be sold
  unique_own  BOOLEAN NOT NULL DEFAULT FALSE    -- hats, titles, kite skins
);

CREATE TABLE IF NOT EXISTS inventory (
  player_id   TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  item_id     TEXT NOT NULL REFERENCES items(id),
  qty         INT  NOT NULL DEFAULT 0 CHECK (qty >= 0),
  PRIMARY KEY (player_id, item_id)
);

CREATE TABLE IF NOT EXISTS team_fund (
  dept        TEXT PRIMARY KEY,
  total       BIGINT NOT NULL DEFAULT 0,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS team_contrib (
  player_id   TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  dept        TEXT NOT NULL,
  amount      BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (player_id, dept)
);

CREATE TABLE IF NOT EXISTS quiz_questions (
  id          BIGSERIAL PRIMARY KEY,
  topic       TEXT NOT NULL,                    -- phishing, password, pdpa, waste, malware ...
  prompt      TEXT NOT NULL,
  choices     JSONB NOT NULL,                   -- ["...","...","..."]
  answer_idx  INT  NOT NULL,
  explain     TEXT NOT NULL DEFAULT '',
  active      BOOLEAN NOT NULL DEFAULT TRUE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Learning record for HR / IT reports
CREATE TABLE IF NOT EXISTS quiz_attempts (
  id          BIGSERIAL PRIMARY KEY,
  player_id   TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  question_id BIGINT NOT NULL REFERENCES quiz_questions(id),
  chosen      INT NOT NULL,
  correct     BOOLEAN NOT NULL,
  ms_to_answer INT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS quiz_attempts_player ON quiz_attempts (player_id, created_at DESC);
CREATE INDEX IF NOT EXISTS quiz_attempts_question ON quiz_attempts (question_id);

CREATE TABLE IF NOT EXISTS economy_config (
  id          INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  value       JSONB NOT NULL,
  updated_by  TEXT,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS anomalies (
  id          BIGSERIAL PRIMARY KEY,
  player_id   TEXT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,
  detail      JSONB NOT NULL DEFAULT '{}',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Defaults mirror the in-game settings panel
INSERT INTO economy_config (id, value) VALUES (1, '{
  "run_daily": 3, "run_min_seconds": 25,
  "kite_daily": 3, "kite_max_score_per_sec": 2.0,
  "fish_daily": 30, "rare_mul": 1.0,
  "balloon_daily": 5, "shell_daily": 12, "crystal_daily": 8, "egg_daily": 6, "hay_daily": 4, "shake_daily": 20,
  "sell_full": 5, "sell_decay_pct": 50, "sell_floor_pct": 20,
  "quiz_reward_daily": 10, "quiz_coins": 5, "quiz_kp": 3,
  "team_levels": [1500,3500,6000,10000,15000,21000,28000,36000,45000,60000]
}') ON CONFLICT (id) DO NOTHING;

INSERT INTO items (id, name, kind, buy_price, buy_currency, sell_price, unique_own) VALUES
  ('fish_nil','ปลานิล','fish',NULL,'coin',8,false),
  ('fish_tapian','ปลาตะเพียนทอง','fish',NULL,'coin',15,false),
  ('fish_kad','ปลากัดหางพระจันทร์','fish',NULL,'coin',30,false),
  ('fish_koi','ปลาคาร์ปทองคำ','fish',NULL,'coin',80,false),
  ('fish_raw','ปลาข้อมูลดิบ','fish',NULL,'coin',8,false),
  ('fish_json','ปลา JSON ลายปีกกา','fish',NULL,'coin',15,false),
  ('fish_bug','ปลาบั๊กตัวร้าย','fish',NULL,'coin',30,false),
  ('fish_whale','วาฬ Big Data','fish',NULL,'coin',80,false),
  ('fish_mackerel','ปลาทู','fish',NULL,'coin',8,false),
  ('fish_seabass','ปลากะพงขาว','fish',NULL,'coin',15,false),
  ('fish_squid','ปลาหมึกกล้วย','fish',NULL,'coin',30,false),
  ('fish_whaleshark','ฉลามวาฬ','fish',NULL,'coin',80,false),
  ('scallop','หอยเชลล์ชมพู','collect',NULL,'coin',4,false),
  ('conch','หอยสังข์','collect',NULL,'coin',10,false),
  ('starfish','ปลาดาว','collect',NULL,'coin',8,false),
  ('pearl','หอยมุก','collect',NULL,'coin',35,false),
  ('mango','มะม่วงน้ำดอกไม้','food',6,'coin',3,false),
  ('seed','เมล็ดข้าวหอม','material',2,'coin',2,false),
  ('crystal','คริสตัลข้อมูล','material',NULL,'coin',12,false),
  ('egg','ไข่ไก่สด','material',NULL,'coin',5,false),
  ('hay','หญ้าแห้ง','material',NULL,'coin',0,false),
  ('tea','ชาไทยเย็น','food',10,'coin',0,false),
  ('hat','หมวกสานสายลม','wear',120,'coin',0,true),
  ('title_guard','ฉายา ผู้พิทักษ์ไซเบอร์','title',30,'kp',0,true),
  ('kp_statue','รูปปั้น Paksa ทองคำ','deco',60,'kp',0,false)
ON CONFLICT (id) DO NOTHING;

COMMIT;
