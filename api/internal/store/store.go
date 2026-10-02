package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInsufficient = errors.New("insufficient balance")
	ErrInProgress   = errors.New("same action is still being processed")
	ErrNoItem       = errors.New("not enough items")
)

var bkk, _ = time.LoadLocation("Asia/Bangkok")

// Today is the game day in Thailand; daily caps reset at local midnight.
func Today() time.Time { y, m, d := time.Now().In(bkk).Date(); return time.Date(y, m, d, 0, 0, 0, 0, bkk) }

type Store struct{ DB *pgxpool.Pool }

// Idempotent runs fn once per (player, key). A repeated key returns the stored first response.
// The placeholder row also serialises concurrent duplicates: the second request waits on the row lock.
func (s *Store) Idempotent(ctx context.Context, playerID, key string, fn func(tx pgx.Tx) (any, error)) (json.RawMessage, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `INSERT INTO action_results (player_id, idem_key, response) VALUES ($1,$2,'null') ON CONFLICT DO NOTHING`, playerID, key)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		_ = tx.Rollback(ctx)
		var prev json.RawMessage
		if err := s.DB.QueryRow(ctx, `SELECT response FROM action_results WHERE player_id=$1 AND idem_key=$2`, playerID, key).Scan(&prev); err != nil {
			return nil, err
		}
		if string(prev) == "null" {
			return nil, ErrInProgress
		}
		return prev, nil
	}
	res, err := fn(tx)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE action_results SET response=$3 WHERE player_id=$1 AND idem_key=$2`, playerID, key, out); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

type Wallet struct {
	Coins int64 `json:"coins"`
	KP    int64 `json:"kp"`
}

// Credit changes a balance and appends to the ledger. Negative delta debits; going below zero fails.
func Credit(ctx context.Context, tx pgx.Tx, playerID, currency string, delta int64, reason, idem string, ref map[string]any, actor *string) (int64, error) {
	if delta == 0 {
		var bal int64
		col := map[string]string{"coin": "coins", "kp": "kp"}[currency]
		err := tx.QueryRow(ctx, `SELECT `+col+` FROM wallets WHERE player_id=$1`, playerID).Scan(&bal)
		return bal, err
	}
	col := map[string]string{"coin": "coins", "kp": "kp"}[currency]
	if col == "" {
		return 0, errors.New("bad currency")
	}
	var bal int64
	err := tx.QueryRow(ctx, `UPDATE wallets SET `+col+`=`+col+`+$2, updated_at=now() WHERE player_id=$1 RETURNING `+col, playerID, delta).Scan(&bal)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" { // check_violation
		return 0, ErrInsufficient
	}
	if err != nil {
		return 0, err
	}
	if ref == nil {
		ref = map[string]any{}
	}
	_, err = tx.Exec(ctx, `INSERT INTO ledger (player_id, currency, delta, balance, reason, ref, idem_key, actor) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		playerID, currency, delta, bal, reason, ref, idem, actor)
	return bal, err
}

func GetWallet(ctx context.Context, q pgx.Tx, playerID string) (Wallet, error) {
	var w Wallet
	err := q.QueryRow(ctx, `SELECT coins, kp FROM wallets WHERE player_id=$1`, playerID).Scan(&w.Coins, &w.KP)
	return w, err
}

// Bump increments a daily counter only while it is below cap. Returns false when the cap is reached.
func Bump(ctx context.Context, tx pgx.Tx, playerID, key string, cap int) (bool, int, error) {
	if cap <= 0 {
		return false, 0, nil
	}
	var n int
	err := tx.QueryRow(ctx, `
		INSERT INTO daily_counters (player_id, day, key, count) VALUES ($1,$2,$3,1)
		ON CONFLICT (player_id, day, key) DO UPDATE SET count = daily_counters.count + 1
		WHERE daily_counters.count < $4
		RETURNING count`, playerID, Today(), key, cap).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, cap, nil
	}
	return err == nil, n, err
}

// Counter reads (and row-locks) a daily counter, then adds `add` to it.
func AddCounter(ctx context.Context, tx pgx.Tx, playerID, key string, add int) (before int, err error) {
	err = tx.QueryRow(ctx, `
		INSERT INTO daily_counters (player_id, day, key, count) VALUES ($1,$2,$3,$4)
		ON CONFLICT (player_id, day, key) DO UPDATE SET count = daily_counters.count + EXCLUDED.count
		RETURNING count - $4`, playerID, Today(), key, add).Scan(&before)
	return
}

func AddItem(ctx context.Context, tx pgx.Tx, playerID, itemID string, qty int) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `
		INSERT INTO inventory (player_id, item_id, qty) VALUES ($1,$2,$3)
		ON CONFLICT (player_id, item_id) DO UPDATE SET qty = inventory.qty + EXCLUDED.qty
		RETURNING qty`, playerID, itemID, qty).Scan(&n)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" {
		return 0, ErrNoItem
	}
	return n, err
}

func Flag(ctx context.Context, tx pgx.Tx, playerID, kind string, detail map[string]any) {
	_, _ = tx.Exec(ctx, `INSERT INTO anomalies (player_id, kind, detail) VALUES ($1,$2,$3)`, playerID, kind, detail)
}
