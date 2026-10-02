package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/ktbgs/midway-quest-api/internal/auth"
	"github.com/ktbgs/midway-quest-api/internal/economy"
	"github.com/ktbgs/midway-quest-api/internal/store"
)

type Handlers struct {
	Svc   *economy.Service
	Redis *redis.Client
}

var idemRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,80}$`)

func Register(app *fiber.App, h *Handlers, v *auth.Verifier) {
	api := app.Group("/api/v1", v.Middleware(), h.rateLimit(120))
	api.Get("/me", h.me)
	api.Post("/quiz/next", h.quizNext)
	api.Post("/quiz/answer", h.write(h.quizAnswer))
	api.Post("/minigame/start", h.minigameStart)
	api.Post("/minigame/finish", h.write(h.minigameFinish))
	api.Post("/collect", h.write(h.collect))
	api.Post("/shop/buy", h.write(h.buy))
	api.Post("/shop/sell", h.write(h.sell))
	api.Post("/team/contribute", h.write(h.contribute))
	api.Get("/team/:dept", h.team)

	adm := api.Group("/admin", auth.RequireAdmin())
	adm.Get("/config", h.getConfig)
	adm.Put("/config", h.putConfig)
	adm.Post("/grant", h.write(h.grant))
	adm.Get("/ledger", h.ledger)
	adm.Get("/anomalies", h.anomalies)
}

// write wraps a state-changing action: requires Idempotency-Key and runs it exactly once in one transaction.
func (h *Handlers) write(fn func(c *fiber.Ctx, tx pgx.Tx, idem string) (any, error)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := c.Get("Idempotency-Key")
		if !idemRe.MatchString(key) {
			return fiber.NewError(fiber.StatusBadRequest, "Idempotency-Key header required")
		}
		p := auth.From(c)
		out, err := h.Svc.Store.Idempotent(c.UserContext(), p.ID, key, func(tx pgx.Tx) (any, error) { return fn(c, tx, key) })
		if err != nil {
			return mapErr(err)
		}
		c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
		return c.Send(out)
	}
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, store.ErrInsufficient):
		return fiber.NewError(fiber.StatusConflict, "เหรียญหรือตราไม่พอ")
	case errors.Is(err, store.ErrNoItem):
		return fiber.NewError(fiber.StatusConflict, "ไอเทมไม่พอ")
	case errors.Is(err, store.ErrInProgress):
		return fiber.NewError(fiber.StatusConflict, "กำลังประมวลผลคำขอเดิม")
	case errors.Is(err, economy.ErrBadSession):
		return fiber.NewError(fiber.StatusGone, "เซสชันหมดอายุหรือถูกใช้แล้ว")
	case errors.Is(err, economy.ErrNotForSale), errors.Is(err, economy.ErrBadRequest):
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	case errors.Is(err, economy.ErrOwned):
		return fiber.NewError(fiber.StatusConflict, "มีของชิ้นนี้แล้ว")
	}
	return err
}

func body[T any](c *fiber.Ctx) (T, error) {
	var v T
	if err := json.Unmarshal(c.Body(), &v); err != nil {
		return v, economy.ErrBadRequest
	}
	return v, nil
}

func (h *Handlers) me(c *fiber.Ctx) error {
	out, err := h.Svc.Me(c.UserContext(), auth.From(c))
	if err != nil {
		return err
	}
	return c.JSON(out)
}

func (h *Handlers) quizNext(c *fiber.Ctx) error {
	in, _ := body[struct{ Topic string `json:"topic"` }](c)
	out, err := h.Svc.NextQuestion(c.UserContext(), auth.From(c), in.Topic)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "no question")
	}
	if err != nil {
		return err
	}
	return c.JSON(out)
}

func (h *Handlers) quizAnswer(c *fiber.Ctx, tx pgx.Tx, idem string) (any, error) {
	in, err := body[struct {
		Token  string `json:"token"`
		Choice int    `json:"choice"`
	}](c)
	if err != nil {
		return nil, err
	}
	return h.Svc.Answer(c.UserContext(), tx, auth.From(c), in.Token, in.Choice, idem)
}

func (h *Handlers) minigameStart(c *fiber.Ctx) error {
	in, err := body[struct {
		Kind string `json:"kind"`
		Spot string `json:"spot"`
	}](c)
	if err != nil {
		return mapErr(err)
	}
	out, err := h.Svc.StartMinigame(c.UserContext(), auth.From(c), in.Kind, in.Spot)
	if err != nil {
		return mapErr(err)
	}
	return c.JSON(out)
}

func (h *Handlers) minigameFinish(c *fiber.Ctx, tx pgx.Tx, idem string) (any, error) {
	in, err := body[economy.FinishInput](c)
	if err != nil {
		return nil, err
	}
	return h.Svc.FinishMinigame(c.UserContext(), tx, auth.From(c), in, idem)
}

func (h *Handlers) collect(c *fiber.Ctx, tx pgx.Tx, idem string) (any, error) {
	in, err := body[struct{ Kind string `json:"kind"` }](c)
	if err != nil {
		return nil, err
	}
	return h.Svc.Collect(c.UserContext(), tx, auth.From(c), in.Kind, idem)
}

type itemQty struct {
	ItemID string `json:"item_id"`
	Qty    int    `json:"qty"`
}

func (h *Handlers) buy(c *fiber.Ctx, tx pgx.Tx, idem string) (any, error) {
	in, err := body[itemQty](c)
	if err != nil {
		return nil, err
	}
	return h.Svc.Buy(c.UserContext(), tx, auth.From(c), in.ItemID, in.Qty, idem)
}

func (h *Handlers) sell(c *fiber.Ctx, tx pgx.Tx, idem string) (any, error) {
	in, err := body[itemQty](c)
	if err != nil {
		return nil, err
	}
	return h.Svc.Sell(c.UserContext(), tx, auth.From(c), in.ItemID, in.Qty, idem)
}

func (h *Handlers) contribute(c *fiber.Ctx, tx pgx.Tx, idem string) (any, error) {
	in, err := body[struct{ Amount int64 `json:"amount"` }](c)
	if err != nil {
		return nil, err
	}
	return h.Svc.Contribute(c.UserContext(), tx, auth.From(c), in.Amount, idem)
}

func (h *Handlers) team(c *fiber.Ctx) error {
	out, err := h.Svc.Team(c.UserContext(), c.Params("dept"))
	if err != nil {
		return err
	}
	return c.JSON(out)
}

// ---------- admin ----------

func (h *Handlers) getConfig(c *fiber.Ctx) error {
	cfg, err := h.Svc.Cfg.Get(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(cfg)
}

func (h *Handlers) putConfig(c *fiber.Ctx) error {
	var cfg economy.Config
	if err := json.Unmarshal(c.Body(), &cfg); err != nil || !cfg.Valid() {
		return fiber.NewError(fiber.StatusBadRequest, "invalid config")
	}
	raw, _ := json.Marshal(cfg)
	_, err := h.Svc.Store.DB.Exec(c.UserContext(), `UPDATE economy_config SET value=$1, updated_by=$2, updated_at=now() WHERE id=1`, raw, auth.From(c).ID)
	if err != nil {
		return err
	}
	h.Svc.Cfg.Invalidate()
	return c.JSON(cfg)
}

func (h *Handlers) grant(c *fiber.Ctx, tx pgx.Tx, idem string) (any, error) {
	in, err := body[struct {
		PlayerID string `json:"player_id"`
		Currency string `json:"currency"`
		Delta    int64  `json:"delta"`
		Note     string `json:"note"`
	}](c)
	if err != nil || in.PlayerID == "" || in.Delta == 0 {
		return nil, economy.ErrBadRequest
	}
	return h.Svc.Grant(c.UserContext(), tx, auth.From(c), in.PlayerID, in.Currency, in.Delta, in.Note, idem)
}

func (h *Handlers) ledger(c *fiber.Ctx) error {
	rows, err := h.Svc.Store.DB.Query(c.UserContext(), `SELECT id, currency, delta, balance, reason, ref, actor, created_at
		FROM ledger WHERE player_id=$1 ORDER BY id DESC LIMIT 500`, c.Query("player"))
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, delta, bal int64
		var cur, reason string
		var ref json.RawMessage
		var actor *string
		var at time.Time
		if err := rows.Scan(&id, &cur, &delta, &bal, &reason, &ref, &actor, &at); err != nil {
			return err
		}
		out = append(out, map[string]any{"id": id, "currency": cur, "delta": delta, "balance": bal, "reason": reason, "ref": ref, "actor": actor, "at": at})
	}
	return c.JSON(out)
}

func (h *Handlers) anomalies(c *fiber.Ctx) error {
	rows, err := h.Svc.Store.DB.Query(c.UserContext(), `SELECT a.id, a.player_id, p.name, p.dept, a.kind, a.detail, a.created_at
		FROM anomalies a JOIN players p ON p.id=a.player_id ORDER BY a.id DESC LIMIT 300`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var pid, name, dept, kind string
		var detail json.RawMessage
		var at time.Time
		if err := rows.Scan(&id, &pid, &name, &dept, &kind, &detail, &at); err != nil {
			return err
		}
		out = append(out, map[string]any{"id": id, "player_id": pid, "name": name, "dept": dept, "kind": kind, "detail": detail, "at": at})
	}
	return c.JSON(out)
}

// rateLimit is a fixed one-minute window per player (Redis INCR).
func (h *Handlers) rateLimit(perMin int64) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if c.Method() == fiber.MethodGet {
			return c.Next()
		}
		key := fmt.Sprintf("mq:rl:%s:%d", auth.From(c).ID, time.Now().Unix()/60)
		n, err := h.Redis.Incr(c.UserContext(), key).Result()
		if err == nil && n == 1 {
			h.Redis.Expire(c.UserContext(), key, 70*time.Second)
		}
		if err == nil && n > perMin {
			return fiber.NewError(fiber.StatusTooManyRequests, "ช้าลงหน่อย")
		}
		return c.Next()
	}
}
