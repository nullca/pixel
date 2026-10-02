package auth

import (
	"context"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Player is the authenticated caller. Identity and department come only from the KID token.
type Player struct {
	ID      string
	Name    string
	Dept    string
	IsAdmin bool
}

// kidClaims — rename json tags here if KID uses different claim names.
type kidClaims struct {
	Name       string   `json:"name"`
	Department string   `json:"department"`
	Roles      []string `json:"roles"`
	jwt.RegisteredClaims
}

type Verifier struct {
	kf        keyfunc.Keyfunc
	issuer    string
	audience  string
	adminRole string
	db        *pgxpool.Pool
}

func NewVerifier(ctx context.Context, jwksURL, issuer, audience, adminRole string, db *pgxpool.Pool) (*Verifier, error) {
	kf, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL}) // refreshes keys in the background
	if err != nil {
		return nil, err
	}
	return &Verifier{kf: kf, issuer: issuer, audience: audience, adminRole: adminRole, db: db}, nil
}

const ctxKey = "player"

// Middleware verifies the bearer token and loads/updates the player row.
func (v *Verifier) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		h := c.Get(fiber.HeaderAuthorization)
		raw, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || raw == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		var cl kidClaims
		_, err := jwt.ParseWithClaims(raw, &cl, v.kf.Keyfunc,
			jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience),
			jwt.WithValidMethods([]string{"RS256", "ES256", "PS256"}),
			jwt.WithLeeway(30*time.Second), jwt.WithExpirationRequired())
		if err != nil || cl.Subject == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid token")
		}
		p := Player{ID: cl.Subject, Name: strings.TrimSpace(cl.Name), Dept: strings.ToUpper(strings.TrimSpace(cl.Department))}
		if p.Dept == "" {
			p.Dept = "UNASSIGNED"
		}
		for _, r := range cl.Roles {
			if r == v.adminRole {
				p.IsAdmin = true
			}
		}
		// Keep the player row in sync with KID (name/department changes in HR system flow through here).
		_, err = v.db.Exec(c.UserContext(), `
			INSERT INTO players (id, name, dept, is_admin) VALUES ($1,$2,$3,$4)
			ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, dept=EXCLUDED.dept, is_admin=EXCLUDED.is_admin, last_seen=now()`,
			p.ID, p.Name, p.Dept, p.IsAdmin)
		if err != nil {
			return err
		}
		if _, err = v.db.Exec(c.UserContext(), `INSERT INTO wallets (player_id) VALUES ($1) ON CONFLICT DO NOTHING`, p.ID); err != nil {
			return err
		}
		c.Locals(ctxKey, p)
		return c.Next()
	}
}

func From(c *fiber.Ctx) Player { return c.Locals(ctxKey).(Player) }

func RequireAdmin() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !From(c).IsAdmin {
			return fiber.NewError(fiber.StatusForbidden, "admin only")
		}
		return c.Next()
	}
}
