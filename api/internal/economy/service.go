package economy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/ktbgs/midway-quest-api/internal/auth"
	"github.com/ktbgs/midway-quest-api/internal/store"
)

var (
	ErrBadSession = errors.New("session not found or already used")
	ErrBadRequest = errors.New("bad request")
	ErrNotForSale = errors.New("item is not for sale")
	ErrOwned      = errors.New("already owned")
)

type Service struct {
	Store *store.Store
	Redis *redis.Client
	Cfg   *ConfigCache
}

type Result struct {
	Reward  map[string]any `json:"reward"`
	Items   map[string]int `json:"items,omitempty"`
	Wallet  store.Wallet   `json:"wallet"`
	Capped  bool           `json:"capped"`
	Message string         `json:"message,omitempty"`
}

// ---------- minigame sessions (Redis, single use) ----------

type session struct {
	PlayerID string    `json:"p"`
	Kind     string    `json:"k"`
	Spot     string    `json:"s,omitempty"`
	Start    time.Time `json:"t"`
}

var minigameKinds = map[string]bool{"run": true, "kite": true, "fish": true}

func (s *Service) StartMinigame(ctx context.Context, p auth.Player, kind, spot string) (map[string]any, error) {
	if !minigameKinds[kind] {
		return nil, ErrBadRequest
	}
	if kind == "fish" {
		if _, ok := fishTable[spot]; !ok {
			return nil, ErrBadRequest
		}
	}
	id := uuid.NewString()
	b, _ := json.Marshal(session{PlayerID: p.ID, Kind: kind, Spot: spot, Start: time.Now()})
	if err := s.Redis.Set(ctx, "mq:sess:"+id, b, 15*time.Minute).Err(); err != nil {
		return nil, err
	}
	return map[string]any{"session_id": id, "started_at": time.Now().UnixMilli()}, nil
}

type FinishInput struct {
	SessionID string `json:"session_id"`
	Score     int    `json:"score"`  // kite: stars; ignored for run
	Caught    bool   `json:"caught"` // fish: true if the reel minigame succeeded
}

func (s *Service) FinishMinigame(ctx context.Context, tx pgx.Tx, p auth.Player, in FinishInput, idem string) (Result, error) {
	raw, err := s.Redis.GetDel(ctx, "mq:sess:"+in.SessionID).Bytes()
	if err != nil {
		return Result{}, ErrBadSession
	}
	var ss session
	if json.Unmarshal(raw, &ss) != nil || ss.PlayerID != p.ID {
		return Result{}, ErrBadSession
	}
	cfg, err := s.Cfg.Get(ctx)
	if err != nil {
		return Result{}, err
	}
	elapsed := time.Since(ss.Start).Seconds()
	res := Result{Reward: map[string]any{}}
	switch ss.Kind {
	case "run":
		secs := elapsed - 3 // 3-second countdown before GO
		if secs < cfg.RunMinSeconds {
			store.Flag(ctx, tx, p.ID, "run_too_fast", map[string]any{"seconds": secs})
			res.Message = "เวลาไม่สมเหตุสมผล ไม่ได้รับรางวัล"
			break
		}
		ok, _, err := store.Bump(ctx, tx, p.ID, "run", cfg.RunDaily)
		if err != nil {
			return res, err
		}
		res.Reward["seconds"] = secs
		if !ok {
			res.Capped = true
			break
		}
		coins := RunCoins(secs)
		if _, err := store.Credit(ctx, tx, p.ID, "coin", coins, "minigame:run", idem, map[string]any{"seconds": secs}, nil); err != nil {
			return res, err
		}
		res.Reward["coins"] = coins
	case "kite":
		maxScore := int(cfg.KiteMaxScorePerSec * elapsed)
		score := in.Score
		if score < 0 {
			score = 0
		}
		if score > maxScore {
			store.Flag(ctx, tx, p.ID, "kite_score_capped", map[string]any{"reported": score, "max": maxScore, "elapsed": elapsed})
			score = maxScore
		}
		ok, _, err := store.Bump(ctx, tx, p.ID, "kite", cfg.KiteDaily)
		if err != nil {
			return res, err
		}
		res.Reward["score"] = score
		if !ok || score == 0 {
			res.Capped = !ok
			break
		}
		coins := KiteCoins(score)
		if _, err := store.Credit(ctx, tx, p.ID, "coin", coins, "minigame:kite", idem, map[string]any{"score": score}, nil); err != nil {
			return res, err
		}
		res.Reward["coins"] = coins
	case "fish":
		if !in.Caught || elapsed < 2 {
			res.Message = "ปลาหลุด"
			break
		}
		ok, _, err := store.Bump(ctx, tx, p.ID, "fish", cfg.FishDaily)
		if err != nil {
			return res, err
		}
		if !ok {
			res.Capped = true
			break
		}
		h := time.Now().Hour()
		id, size, tier, _ := RollFish(ss.Spot, cfg, h >= 19 || h < 6)
		if _, err := store.AddItem(ctx, tx, p.ID, id, 1); err != nil {
			return res, err
		}
		res.Items = map[string]int{id: 1}
		res.Reward["fish"] = map[string]any{"id": id, "size_cm": size, "tier": tier}
	}
	w, err := store.GetWallet(ctx, tx, p.ID)
	res.Wallet = w
	return res, err
}

// ---------- quiz: questions are served without answers, graded on the server ----------

type quizToken struct {
	PlayerID string    `json:"p"`
	QID      int64     `json:"q"`
	At       time.Time `json:"t"`
}

func (s *Service) NextQuestion(ctx context.Context, p auth.Player, topic string) (map[string]any, error) {
	var id int64
	var prompt string
	var choices json.RawMessage
	// Prefer questions this player got wrong before (spaced repetition), then unseen, then random.
	err := s.Store.DB.QueryRow(ctx, `
		SELECT q.id, q.prompt, q.choices FROM quiz_questions q
		LEFT JOIN LATERAL (SELECT bool_or(NOT correct) AS wrong, count(*) AS n, max(created_at) AS last
		                   FROM quiz_attempts a WHERE a.player_id=$1 AND a.question_id=q.id) h ON true
		WHERE q.active AND ($2='' OR q.topic=$2)
		ORDER BY (COALESCE(h.wrong,false) AND h.last < now() - interval '1 day') DESC, COALESCE(h.n,0) ASC, random()
		LIMIT 1`, p.ID, topic).Scan(&id, &prompt, &choices)
	if err != nil {
		return nil, err
	}
	tok := uuid.NewString()
	b, _ := json.Marshal(quizToken{PlayerID: p.ID, QID: id, At: time.Now()})
	if err := s.Redis.Set(ctx, "mq:quiz:"+tok, b, 10*time.Minute).Err(); err != nil {
		return nil, err
	}
	return map[string]any{"token": tok, "prompt": prompt, "choices": choices}, nil
}

func (s *Service) Answer(ctx context.Context, tx pgx.Tx, p auth.Player, token string, choice int, idem string) (map[string]any, error) {
	raw, err := s.Redis.GetDel(ctx, "mq:quiz:"+token).Bytes()
	if err != nil {
		return nil, ErrBadSession
	}
	var qt quizToken
	if json.Unmarshal(raw, &qt) != nil || qt.PlayerID != p.ID {
		return nil, ErrBadSession
	}
	var ans int
	var explain string
	if err := tx.QueryRow(ctx, `SELECT answer_idx, explain FROM quiz_questions WHERE id=$1`, qt.QID).Scan(&ans, &explain); err != nil {
		return nil, err
	}
	correct := choice == ans
	ms := int(time.Since(qt.At).Milliseconds())
	if _, err := tx.Exec(ctx, `INSERT INTO quiz_attempts (player_id, question_id, chosen, correct, ms_to_answer) VALUES ($1,$2,$3,$4,$5)`, p.ID, qt.QID, choice, correct, ms); err != nil {
		return nil, err
	}
	if correct && ms < 800 {
		store.Flag(ctx, tx, p.ID, "quiz_answer_too_fast", map[string]any{"ms": ms, "q": qt.QID})
	}
	out := map[string]any{"correct": correct, "answer": ans, "explain": explain, "reward": map[string]any{}}
	if correct {
		cfg, err := s.Cfg.Get(ctx)
		if err != nil {
			return nil, err
		}
		if ok, _, err := store.Bump(ctx, tx, p.ID, "quiz_reward", cfg.QuizRewardDaily); err != nil {
			return nil, err
		} else if ok {
			if _, err := store.Credit(ctx, tx, p.ID, "coin", cfg.QuizCoins, "quiz:correct", idem, map[string]any{"q": qt.QID}, nil); err != nil {
				return nil, err
			}
			if _, err := store.Credit(ctx, tx, p.ID, "kp", cfg.QuizKP, "quiz:correct", idem, map[string]any{"q": qt.QID}, nil); err != nil {
				return nil, err
			}
			out["reward"] = map[string]any{"coins": cfg.QuizCoins, "kp": cfg.QuizKP}
		} else {
			out["capped"] = true
		}
	}
	w, err := store.GetWallet(ctx, tx, p.ID)
	out["wallet"] = w
	return out, err
}

// ---------- collecting things in the world ----------

func (s *Service) Collect(ctx context.Context, tx pgx.Tx, p auth.Player, kind, idem string) (Result, error) {
	rule, ok := CollectRules[kind]
	if !ok {
		return Result{}, ErrBadRequest
	}
	cfg, err := s.Cfg.Get(ctx)
	if err != nil {
		return Result{}, err
	}
	res := Result{Reward: map[string]any{}}
	allowed, _, err := store.Bump(ctx, tx, p.ID, "collect:"+kind, rule.Cap(cfg))
	if err != nil {
		return res, err
	}
	if !allowed {
		res.Capped = true
	} else {
		if rule.Coins > 0 {
			if _, err := store.Credit(ctx, tx, p.ID, "coin", rule.Coins, "collect:"+kind, idem, nil, nil); err != nil {
				return res, err
			}
			res.Reward["coins"] = rule.Coins
		}
		if len(rule.Items) > 0 {
			if id := pick(rule.Items); id != "" {
				if _, err := store.AddItem(ctx, tx, p.ID, id, 1); err != nil {
					return res, err
				}
				res.Items = map[string]int{id: 1}
			} else if kind == "shake" && rand.IntN(2) == 0 {
				if _, err := store.Credit(ctx, tx, p.ID, "coin", 5, "collect:shake", idem, nil, nil); err != nil {
					return res, err
				}
				res.Reward["coins"] = 5
			}
		}
	}
	res.Wallet, err = store.GetWallet(ctx, tx, p.ID)
	return res, err
}

// ---------- shop / sell ----------

func (s *Service) Buy(ctx context.Context, tx pgx.Tx, p auth.Player, itemID string, qty int, idem string) (Result, error) {
	if qty < 1 || qty > 99 {
		return Result{}, ErrBadRequest
	}
	var price *int64
	var cur string
	var unique bool
	if err := tx.QueryRow(ctx, `SELECT buy_price, buy_currency, unique_own FROM items WHERE id=$1`, itemID).Scan(&price, &cur, &unique); err != nil {
		return Result{}, ErrBadRequest
	}
	if price == nil {
		return Result{}, ErrNotForSale
	}
	if unique {
		qty = 1
		var have int
		_ = tx.QueryRow(ctx, `SELECT qty FROM inventory WHERE player_id=$1 AND item_id=$2`, p.ID, itemID).Scan(&have)
		if have > 0 {
			return Result{}, ErrOwned
		}
	}
	if _, err := store.Credit(ctx, tx, p.ID, cur, -*price*int64(qty), "shop:buy", idem, map[string]any{"item": itemID, "qty": qty}, nil); err != nil {
		return Result{}, err
	}
	if _, err := store.AddItem(ctx, tx, p.ID, itemID, qty); err != nil {
		return Result{}, err
	}
	w, err := store.GetWallet(ctx, tx, p.ID)
	return Result{Items: map[string]int{itemID: qty}, Wallet: w, Reward: map[string]any{}}, err
}

func (s *Service) Sell(ctx context.Context, tx pgx.Tx, p auth.Player, itemID string, qty int, idem string) (Result, error) {
	if qty < 1 || qty > 999 {
		return Result{}, ErrBadRequest
	}
	var base int64
	if err := tx.QueryRow(ctx, `SELECT sell_price FROM items WHERE id=$1`, itemID).Scan(&base); err != nil || base <= 0 {
		return Result{}, ErrNotForSale
	}
	if _, err := store.AddItem(ctx, tx, p.ID, itemID, -qty); err != nil {
		return Result{}, err
	}
	cfg, err := s.Cfg.Get(ctx)
	if err != nil {
		return Result{}, err
	}
	before, err := store.AddCounter(ctx, tx, p.ID, "sold:"+itemID, qty)
	if err != nil {
		return Result{}, err
	}
	var total int64
	for i := 0; i < qty; i++ {
		total += UnitPrice(base, before+i, cfg)
	}
	if _, err := store.Credit(ctx, tx, p.ID, "coin", total, "shop:sell", idem, map[string]any{"item": itemID, "qty": qty}, nil); err != nil {
		return Result{}, err
	}
	w, err := store.GetWallet(ctx, tx, p.ID)
	return Result{Reward: map[string]any{"coins": total}, Wallet: w}, err
}

// ---------- team fund ----------

func (s *Service) Contribute(ctx context.Context, tx pgx.Tx, p auth.Player, amount int64, idem string) (map[string]any, error) {
	if amount < 1 || amount > 1_000_000 {
		return nil, ErrBadRequest
	}
	if _, err := store.Credit(ctx, tx, p.ID, "coin", -amount, "team:contribute", idem, map[string]any{"dept": p.Dept}, nil); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO team_contrib (player_id, dept, amount) VALUES ($1,$2,$3)
		ON CONFLICT (player_id, dept) DO UPDATE SET amount = team_contrib.amount + EXCLUDED.amount`, p.ID, p.Dept, amount); err != nil {
		return nil, err
	}
	var total int64
	if err := tx.QueryRow(ctx, `INSERT INTO team_fund (dept, total) VALUES ($1,$2)
		ON CONFLICT (dept) DO UPDATE SET total = team_fund.total + EXCLUDED.total, updated_at = now() RETURNING total`, p.Dept, amount).Scan(&total); err != nil {
		return nil, err
	}
	cfg, err := s.Cfg.Get(ctx)
	if err != nil {
		return nil, err
	}
	w, err := store.GetWallet(ctx, tx, p.ID)
	return map[string]any{"dept": p.Dept, "total": total, "level": TeamLevel(total, cfg.TeamLevels), "wallet": w}, err
}

func (s *Service) Team(ctx context.Context, dept string) (map[string]any, error) {
	var total int64
	_ = s.Store.DB.QueryRow(ctx, `SELECT total FROM team_fund WHERE dept=$1`, dept).Scan(&total)
	cfg, err := s.Cfg.Get(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Store.DB.Query(ctx, `SELECT p.name, c.amount FROM team_contrib c JOIN players p ON p.id=c.player_id
		WHERE c.dept=$1 AND c.amount>0 ORDER BY c.amount DESC LIMIT 10`, dept)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	top := []map[string]any{}
	for rows.Next() {
		var n string
		var a int64
		if err := rows.Scan(&n, &a); err != nil {
			return nil, err
		}
		top = append(top, map[string]any{"name": n, "amount": a})
	}
	return map[string]any{"dept": dept, "total": total, "level": TeamLevel(total, cfg.TeamLevels), "levels": cfg.TeamLevels, "top": top}, rows.Err()
}

// ---------- admin ----------

func (s *Service) Grant(ctx context.Context, tx pgx.Tx, admin auth.Player, playerID, currency string, delta int64, note, idem string) (map[string]any, error) {
	if currency != "coin" && currency != "kp" {
		return nil, ErrBadRequest
	}
	actor := admin.ID
	bal, err := store.Credit(ctx, tx, playerID, currency, delta, "admin:grant", idem, map[string]any{"note": note}, &actor)
	if err != nil {
		return nil, err
	}
	return map[string]any{"player_id": playerID, "currency": currency, "balance": bal}, nil
}

func (s *Service) Me(ctx context.Context, p auth.Player) (map[string]any, error) {
	db := s.Store.DB
	var w store.Wallet
	if err := db.QueryRow(ctx, `SELECT coins, kp FROM wallets WHERE player_id=$1`, p.ID).Scan(&w.Coins, &w.KP); err != nil {
		return nil, err
	}
	inv := map[string]int{}
	rows, err := db.Query(ctx, `SELECT item_id, qty FROM inventory WHERE player_id=$1 AND qty>0`, p.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var q int
		if err := rows.Scan(&id, &q); err != nil {
			rows.Close()
			return nil, err
		}
		inv[id] = q
	}
	rows.Close()
	counters := map[string]int{}
	rows, err = db.Query(ctx, `SELECT key, count FROM daily_counters WHERE player_id=$1 AND day=$2`, p.ID, store.Today())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			rows.Close()
			return nil, err
		}
		counters[k] = n
	}
	rows.Close()
	cfg, err := s.Cfg.Get(ctx)
	if err != nil {
		return nil, err
	}
	team, err := s.Team(ctx, p.Dept)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"player":   map[string]any{"id": p.ID, "name": p.Name, "dept": p.Dept, "admin": p.IsAdmin},
		"wallet":   w, "inventory": inv, "today": counters, "config": cfg, "team": team,
		"server_time": time.Now().UnixMilli(), "version": fmt.Sprintf("v1-%d", time.Now().Year()),
	}, nil
}
