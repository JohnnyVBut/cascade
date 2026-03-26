// AWG-Easy 3.0 — Go/Fiber entry point.
// All managers are initialised in FIX-13 order before the HTTP server starts.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/JohnnyVBut/cascade/internal/aliases"
	"github.com/JohnnyVBut/cascade/internal/api"
	"github.com/JohnnyVBut/cascade/internal/db"
	"github.com/JohnnyVBut/cascade/internal/firewall"
	"github.com/JohnnyVBut/cascade/internal/frontend"
	"github.com/JohnnyVBut/cascade/internal/gateway"
	"github.com/JohnnyVBut/cascade/internal/ipset"
	"github.com/JohnnyVBut/cascade/internal/nat"
	"github.com/JohnnyVBut/cascade/internal/routing"
	"github.com/JohnnyVBut/cascade/internal/tunnel"
	"github.com/JohnnyVBut/cascade/internal/users"
)

// Config holds all runtime configuration resolved from flags and ENV.
// Flag takes priority over ENV (standard Go service pattern).
type Config struct {
	DataDir      string // --data-dir / DATA_DIR
	Port         int    // --port / PORT         (TCP, Web UI)
	BindHost     string // --bind / BIND_ADDR    (listen host, default "" = 0.0.0.0)
	WGPort       int    // --wg-port / WG_PORT   (UDP, WireGuard default)
	Host         string // --host / WG_HOST      (required)
	PasswordHash string // --password-hash / PASSWORD_HASH
	Debug        bool   // --debug / DEBUG
}

func main() {
	cfg := parseConfig()

	// ── Database ──────────────────────────────────────────────────────────────
	// Must be first: all managers depend on db.DB().
	if err := db.Init(cfg.DataDir); err != nil {
		log.Fatalf("db init: %v", err)
	}
	defer db.Close()

	// ── Auth subsystem ────────────────────────────────────────────────────────
	// Initialise before registering routes so middleware is ready.
	api.InitAuth(cfg.PasswordHash)

	// Seed the admin user from PASSWORD_HASH env if the users table is empty.
	// After this point PASSWORD_HASH is only used for the initial seed —
	// subsequent logins use the users table directly.
	if cfg.PasswordHash != "" {
		if err := users.SeedAdminIfEmpty(cfg.PasswordHash); err != nil {
			log.Printf("user seed warning: %v", err)
		}
	}

	// ── Fiber app + middleware ────────────────────────────────────────────────
	app := fiber.New(fiber.Config{
		AppName:               "AWG-Easy 3.0",
		DisableStartupMessage: true,
		ReadTimeout:           30 * time.Second,
		WriteTimeout:          30 * time.Second,
		IdleTimeout:           60 * time.Second,
		ErrorHandler:          errorHandler,
	})

	// Panic recovery — turns panics into HTTP 500 without crashing the server.
	app.Use(recover.New())

	// Request logging: log mutations (POST/PATCH/DELETE/PUT) and errors (4xx/5xx).
	// Successful GET requests (200-399) are never logged — they occur every second
	// from the frontend setInterval polling and would spam the container log.
	app.Use(func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		status := c.Response().StatusCode()
		method := c.Method()
		if method != "GET" || status >= 400 {
			log.Printf("[%s] %s %s → %d (%s)",
				time.Now().Format("15:04:05"),
				method, c.Path(), status,
				time.Since(start).Round(time.Microsecond),
			)
		}
		return err
	})

	// ── API routes ────────────────────────────────────────────────────────────
	// Must be registered BEFORE the static middleware so /api/* requests are
	// handled by the API handlers and not swallowed by the SPA fallback.
	apiGroup := app.Group("/api")

	// ── Unprotected routes (health + auth) ───────────────────────────────────
	apiGroup.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":  "ok",
			"version": "3.0.0-alpha",
			"host":    cfg.Host,
		})
	})

	// Session login/logout — intentionally not behind AuthMiddleware.
	api.RegisterAuth(apiGroup)

	// Legacy shims that are safe without auth (lang, release, feature flags).
	api.RegisterCompat(apiGroup)

	// ── Auth gate — all routes below require authentication ───────────────────
	apiGroup.Use(api.AuthMiddleware)

	// Users management (multi-user auth + TOTP setup).
	api.RegisterUsers(apiGroup)

	// API tokens (programmatic access without session/TOTP).
	api.RegisterTokens(apiGroup)

	// Settings + Templates (registered before other managers are ready, but
	// settings package only needs db which is already initialised above).
	api.RegisterSettings(apiGroup)

	// Remaining handlers are registered here; they call package-level Get()
	// which is safe after SetInstance calls below.
	api.RegisterInterfaces(apiGroup)
	api.RegisterPeers(apiGroup)
	api.RegisterRouting(apiGroup)
	api.RegisterNat(apiGroup)
	api.RegisterAliases(apiGroup)
	api.RegisterFirewall(apiGroup)
	api.RegisterGateways(apiGroup)

	// Legacy shims that require auth (old wireguard/client list → empty array).
	api.RegisterCompatAuth(apiGroup)

	// ── Static files (embed.FS) ───────────────────────────────────────────────
	// Registered AFTER all /api/* routes so the SPA fallback (index.html) does
	// not intercept API requests.
	// Frontend is embedded into the binary at compile time — no disk files needed.
	app.Use("/", filesystem.New(filesystem.Config{
		Root:         frontend.FS(),
		Index:        "index.html",
		Browse:       false,
		NotFoundFile: "index.html", // unknown paths → SPA, Vue handles routing
	}))

	// ── Manager initialisation (FIX-13: strict order) ─────────────────────────
	//
	// Order: ipset/aliases/gateway/firewall are independent of wg interfaces.
	// tunnel.Init brings up all wg interfaces synchronously.
	// routing.RestoreAll and nat.RestoreAll are called AFTER tunnel.Init so that
	// the wg interfaces exist before we add routes/NAT rules to them.
	//
	// 1. IpsetManager — no kernel ops, just data dir setup.
	ipsetMgr, err := ipset.New(cfg.DataDir)
	if err != nil {
		log.Fatalf("ipset init: %v", err)
	}

	// 2. AliasManager — depends on IpsetManager.
	aliasMgr := aliases.New(ipsetMgr)
	aliases.SetInstance(aliasMgr)

	// 3. GatewayManager — independent of wg interfaces.
	gwMgr := gateway.NewManager()
	if err := gwMgr.Init(); err != nil {
		log.Printf("gateway init warning: %v", err)
	}
	gateway.SetInstance(gwMgr)

	// 4. FirewallManager — depends on AliasManager + GatewayManager.
	fwMgr := firewall.New(aliasMgr, gwMgr)
	if err := fwMgr.Init(); err != nil {
		log.Printf("firewall init warning: %v", err)
	}
	firewall.SetInstance(fwMgr)

	// 5. InterfaceManager — brings up all wg/awg interfaces synchronously.
	//    Must complete before RestoreAll() calls below.
	if _, err := tunnel.Init(cfg.Host); err != nil {
		log.Fatalf("tunnel interface manager init: %v", err)
	}

	// 5b. FirewallManager — rebuild PBR routing chains NOW that wg interfaces
	//     are up. The Init() call above (step 4) created iptables chains and
	//     registered the gateway-monitor callback, but applyRoutingForRule()
	//     failed for any rule whose dev= interface did not exist yet.
	//     Re-running RebuildChains() here guarantees "ip route replace default
	//     via X dev wgY table N" executes with wgY already present.
	if err := fwMgr.RebuildChains(); err != nil {
		log.Printf("firewall post-tunnel rebuildChains warning: %v", err)
	}

	// 6. RouteManager — RestoreAll() adds kernel routes AFTER interfaces exist.
	rmgr := routing.New()
	rmgr.RestoreAll()
	routing.SetInstance(rmgr)

	// 7. NatManager — RestoreAll() applies iptables rules AFTER interfaces exist.
	natMgr := nat.New(aliasMgr)
	natMgr.RestoreAll()
	nat.SetInstance(natMgr)

	// ── Start HTTP server ──────────────────────────────────────────────────────
	// cfg.BindHost="" → ":port" → listens on all interfaces (0.0.0.0).
	// cfg.BindHost="127.0.0.1" → "127.0.0.1:port" → localhost only (behind reverse proxy).
	addr := fmt.Sprintf("%s:%d", cfg.BindHost, cfg.Port)
	log.Printf("Cascade | host=%s | listen=%s (tcp) | wg-port=%d (udp) | data=%s",
		cfg.Host, addr, cfg.WGPort, cfg.DataDir)

	// Run in a goroutine so the signal wait below is not blocked.
	go func() {
		if err := app.Listen(addr); err != nil {
			log.Fatalf("server: %v", err)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Println("Shutting down gracefully...")

	// Stop tunnel manager first: closes stopCh → polling goroutine runs final
	// FlushTrafficTotals() before exiting → traffic totals saved to SQLite.
	// Must happen before db.Close() so the DB is still open during the flush.
	if mgr := tunnel.Get(); mgr != nil {
		mgr.Stop()
	}

	if err := app.Shutdown(); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	db.Close()
	log.Println("Bye.")
}

// parseConfig resolves configuration from CLI flags with ENV fallback.
// Flag always wins over ENV — standard pattern for Go services.
func parseConfig() Config {
	var cfg Config

	flag.StringVar(&cfg.DataDir, "data-dir",
		envStr("DATA_DIR", "/etc/wireguard/data"),
		"Path to data directory (JSON storage)")

	flag.IntVar(&cfg.Port, "port",
		envInt("PORT", 8888),
		"Web UI listen port (TCP)")

	flag.StringVar(&cfg.BindHost, "bind",
		envStr("BIND_ADDR", ""),
		"Web UI listen host (default empty = 0.0.0.0; set 127.0.0.1 when behind reverse proxy)")

	flag.IntVar(&cfg.WGPort, "wg-port",
		envInt("WG_PORT", 555),
		"Default WireGuard/AWG listen port (UDP) for new interfaces")

	flag.StringVar(&cfg.Host, "host",
		envStr("WG_HOST", ""),
		"Server public hostname or IP address (optional — can be configured via Settings UI)")

	flag.StringVar(&cfg.PasswordHash, "password-hash",
		envStr("PASSWORD_HASH", ""),
		"bcrypt password hash for Web UI login")

	flag.BoolVar(&cfg.Debug, "debug",
		envBool("DEBUG", false),
		"Enable debug request logging")

	flag.Parse()

	if cfg.Host == "" {
		log.Println("WG_HOST not set — public IP will be resolved via Settings UI or auto-detect")
	}

	return cfg
}

// errorHandler converts errors to JSON responses.
// *fiber.Error (e.g. fiber.NewError(400, "...")) → uses that status code.
// Everything else → 500 Internal Server Error.
func errorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	msg := "Internal Server Error"

	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
		msg = e.Message
	}

	return c.Status(code).JSON(fiber.Map{"error": msg})
}

// ── ENV helpers ───────────────────────────────────────────────────────────────

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
