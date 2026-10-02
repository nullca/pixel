package economy

import (
	"context"
	"encoding/json"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config mirrors economy_config.value. Admins edit it through /admin/config.
type Config struct {
	RunDaily           int     `json:"run_daily"`
	RunMinSeconds      float64 `json:"run_min_seconds"`
	KiteDaily          int     `json:"kite_daily"`
	KiteMaxScorePerSec float64 `json:"kite_max_score_per_sec"`
	FishDaily          int     `json:"fish_daily"`
	RareMul            float64 `json:"rare_mul"`
	BalloonDaily       int     `json:"balloon_daily"`
	ShellDaily         int     `json:"shell_daily"`
	CrystalDaily       int     `json:"crystal_daily"`
	EggDaily           int     `json:"egg_daily"`
	HayDaily           int     `json:"hay_daily"`
	ShakeDaily         int     `json:"shake_daily"`
	SellFull           int     `json:"sell_full"`
	SellDecayPct       int     `json:"sell_decay_pct"`
	SellFloorPct       int     `json:"sell_floor_pct"`
	QuizRewardDaily    int     `json:"quiz_reward_daily"`
	QuizCoins          int64   `json:"quiz_coins"`
	QuizKP             int64   `json:"quiz_kp"`
	TeamLevels         []int64 `json:"team_levels"`
}

func (c Config) Valid() bool {
	return c.RunMinSeconds >= 5 && c.KiteMaxScorePerSec > 0 && c.SellDecayPct >= 0 && c.SellDecayPct <= 100 &&
		c.SellFloorPct >= 0 && c.SellFloorPct <= 100 && c.RareMul >= 0 && c.RareMul <= 10 && len(c.TeamLevels) == 10
}

type ConfigCache struct {
	db  *pgxpool.Pool
	mu  sync.RWMutex
	cfg Config
	at  time.Time
}

func NewConfigCache(db *pgxpool.Pool) *ConfigCache { return &ConfigCache{db: db} }

func (cc *ConfigCache) Get(ctx context.Context) (Config, error) {
	cc.mu.RLock()
	if time.Since(cc.at) < 30*time.Second {
		c := cc.cfg
		cc.mu.RUnlock()
		return c, nil
	}
	cc.mu.RUnlock()
	var raw []byte
	if err := cc.db.QueryRow(ctx, `SELECT value FROM economy_config WHERE id=1`).Scan(&raw); err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return Config{}, err
	}
	cc.mu.Lock()
	cc.cfg, cc.at = c, time.Now()
	cc.mu.Unlock()
	return c, nil
}

func (cc *ConfigCache) Invalidate() { cc.mu.Lock(); cc.at = time.Time{}; cc.mu.Unlock() }

// ---- reward formulas (single source of truth; the client only displays them) ----

// RunCoins uses the server-measured time, never the client's stopwatch.
func RunCoins(seconds float64) int64 {
	return int64(math.Max(15, math.Min(145, math.Round(160-seconds))))
}

func KiteCoins(score int) int64 { return int64(math.Min(80, math.Round(float64(score)*1.5))) }

// UnitPrice applies the per-day sell decay: full price for the first SellFull units of an item,
// then SellDecayPct, then SellFloorPct after twice the quota.
func UnitPrice(base int64, soldBefore int, c Config) int64 {
	switch {
	case soldBefore < c.SellFull:
		return base
	case soldBefore < c.SellFull*2:
		return max64(1, base*int64(c.SellDecayPct)/100)
	default:
		return max64(1, base*int64(c.SellFloorPct)/100)
	}
}

func TeamLevel(total int64, levels []int64) int {
	n := 0
	for _, t := range levels {
		if total >= t {
			n++
		}
	}
	return n
}

type fish struct {
	ID     string
	Weight float64
	Tier   int
	Min    int
	Max    int
}

var fishTable = map[string][]fish{
	"pond": {{"fish_nil", 50, 0, 15, 45}, {"fish_tapian", 30, 1, 10, 30}, {"fish_kad", 14, 2, 4, 7}, {"fish_koi", 6, 3, 35, 80}},
	"lake": {{"fish_raw", 40, 0, 20, 50}, {"fish_json", 28, 1, 25, 60}, {"fish_bug", 14, 2, 10, 25}, {"fish_whale", 6, 3, 400, 900}},
	"sea":  {{"fish_mackerel", 40, 0, 15, 25}, {"fish_seabass", 26, 1, 40, 90}, {"fish_squid", 16, 2, 15, 35}, {"fish_whaleshark", 6, 3, 500, 1200}},
}

// RollFish picks the catch on the server so the client cannot choose a rare species.
func RollFish(spot string, c Config, night bool) (id string, sizeCM int, tier int, ok bool) {
	list, ok := fishTable[spot]
	if !ok {
		return "", 0, 0, false
	}
	boost := 1.0
	if night {
		boost = 1.8
	}
	total := 0.0
	w := make([]float64, len(list))
	for i, f := range list {
		w[i] = f.Weight
		if f.Tier >= 2 {
			w[i] *= boost * c.RareMul
		}
		total += w[i]
	}
	r := rand.Float64() * total
	for i, f := range list {
		r -= w[i]
		if r <= 0 {
			return f.ID, f.Min + rand.IntN(f.Max-f.Min+1), f.Tier, true
		}
	}
	f := list[0]
	return f.ID, f.Min, f.Tier, true
}

// Collect rules: daily cap key, and what one pickup gives.
type CollectRule struct {
	Cap   func(Config) int
	Coins int64
	Items []weighted
}
type weighted struct {
	ID string
	W  float64
}

var CollectRules = map[string]CollectRule{
	"balloon": {Cap: func(c Config) int { return c.BalloonDaily }, Coins: 15},
	"shell":   {Cap: func(c Config) int { return c.ShellDaily }, Items: []weighted{{"scallop", 48}, {"conch", 22}, {"starfish", 24}, {"pearl", 4}}},
	"crystal": {Cap: func(c Config) int { return c.CrystalDaily }, Items: []weighted{{"crystal", 1}}},
	"egg":     {Cap: func(c Config) int { return c.EggDaily }, Items: []weighted{{"egg", 1}}},
	"hay":     {Cap: func(c Config) int { return c.HayDaily }, Items: []weighted{{"hay", 1}}},
	"shake":   {Cap: func(c Config) int { return c.ShakeDaily }, Coins: 0, Items: []weighted{{"mango", 55}, {"seed", 25}, {"", 20}}},
}

func pick(ws []weighted) string {
	t := 0.0
	for _, w := range ws {
		t += w.W
	}
	r := rand.Float64() * t
	for _, w := range ws {
		r -= w.W
		if r <= 0 {
			return w.ID
		}
	}
	return ws[0].ID
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
