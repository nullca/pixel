package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/ktbgs/midway-quest-api/internal/api"
	"github.com/ktbgs/midway-quest-api/internal/auth"
	"github.com/ktbgs/midway-quest-api/internal/config"
	"github.com/ktbgs/midway-quest-api/internal/economy"
	"github.com/ktbgs/midway-quest-api/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer rdb.Close()

	verifier, err := auth.NewVerifier(ctx, cfg.KIDJWKSURL, cfg.KIDIssuer, cfg.KIDAudience, cfg.KIDAdminRole, db)
	if err != nil {
		log.Fatal("KID JWKS: ", err)
	}

	app := fiber.New(fiber.Config{
		AppName:      "midway-quest-api",
		BodyLimit:    64 * 1024,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			var fe *fiber.Error
			if errors.As(err, &fe) {
				code = fe.Code
			} else {
				log.Printf("error %s %s: %v", c.Method(), c.Path(), err)
			}
			msg := "internal error"
			if code < 500 {
				msg = err.Error()
			}
			return c.Status(code).JSON(fiber.Map{"error": msg})
		},
	})
	app.Use(recover.New())
	app.Use(cors.New(cors.Config{AllowOrigins: cfg.AllowedOrigins, AllowHeaders: "Authorization, Content-Type, Idempotency-Key", AllowMethods: "GET,POST,PUT"}))
	app.Get("/healthz", func(c *fiber.Ctx) error { return c.SendString("ok") })

	svc := &economy.Service{Store: &store.Store{DB: db}, Redis: rdb, Cfg: economy.NewConfigCache(db)}
	api.Register(app, &api.Handlers{Svc: svc, Redis: rdb}, verifier)

	go func() {
		<-ctx.Done()
		_ = app.ShutdownWithTimeout(5 * time.Second)
	}()
	log.Printf("listening on :%s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
