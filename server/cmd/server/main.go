package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"aegis-voice/server/internal/api"
	"aegis-voice/server/internal/auth"
	"aegis-voice/server/internal/mcp"
	"aegis-voice/server/internal/rag"
	"aegis-voice/server/internal/rtc"
	"aegis-voice/server/internal/security"
)

func main() {
	logger := security.NewRedactingLogger(os.Stdout)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	vault, err := security.NewVaultFromEnv(os.Getenv("MASTER_ENCRYPTION_KEY"))
	if err != nil {
		logger.Error("invalid MASTER_ENCRYPTION_KEY", "err", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		cancel()
		logger.Error("failed to connect to postgres", "err", err)
		os.Exit(1)
	}
	if err := pool.Ping(ctx); err != nil {
		cancel()
		logger.Error("postgres ping failed", "err", err)
		os.Exit(1)
	}

	migDir := os.Getenv("MIGRATIONS_DIR")
	if migDir == "" {
		migDir = "/app/migrations"
	}
	if err := applyMigrations(ctx, pool, migDir); err != nil {
		cancel()
		logger.Error("failed to apply migrations", "err", err)
		os.Exit(1)
	}
	cancel()

	allowPrivate := strings.EqualFold(os.Getenv("ALLOW_PRIVATE_AI_ENDPOINTS"), "true")
	cookieSecure := strings.EqualFold(os.Getenv("COOKIE_SECURE"), "true")
	webOrigin := os.Getenv("WEB_ORIGIN")
	if webOrigin == "" {
		webOrigin = "http://localhost:3000"
	}

	udpMuxPort, _ := strconv.Atoi(os.Getenv("ICE_UDP_MUX_PORT"))
	var stunURLs []string
	if rawStun := os.Getenv("STUN_URLS"); rawStun != "" {
		for _, s := range strings.Split(rawStun, ",") {
			if t := strings.TrimSpace(s); t != "" {
				stunURLs = append(stunURLs, t)
			}
		}
	}

	rtcMgr, err := rtc.NewManager(rtc.Config{
		UDPMuxPort: udpMuxPort,
		PublicIP:   os.Getenv("ICE_PUBLIC_IP"),
		STUNURLs:   stunURLs,
	}, logger)
	if err != nil {
		logger.Error("failed to initialize Pion WebRTC manager", "err", err)
		os.Exit(1)
	}
	defer rtcMgr.Close()

	authStore := auth.NewPGStore(pool)
	authSvc := auth.NewService(authStore, auth.Config{
		WebOrigin:           webOrigin,
		CookieSecure:        cookieSecure,
		AdminBootstrapToken: os.Getenv("ADMIN_BOOTSTRAP_TOKEN"),
	}, logger)
	authSvc.SetOnSessionRevoked(func(userID, sessionID string) {
		if sessionID != "" {
			rtcMgr.RevokeBySession(sessionID)
		} else {
			rtcMgr.RevokeByUser(userID)
		}
	})

	adminStore := api.NewPGAdminStore(pool)
	ragSvc := rag.NewService(rag.NewPGStore(pool), rag.DefaultMaxUploadBytes)
	mcpHost := mcp.NewHost(allowPrivate)

	apiSrv := api.NewServer(authSvc, authStore, adminStore, ragSvc, rtcMgr, mcpHost, vault, allowPrivate, logger)

	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}
	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           apiSrv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("aegis-server listening", "port", port, "udpMuxPort", udpMuxPort)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "err", err)
			os.Exit(1)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	pool.Close()
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var upFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			upFiles = append(upFiles, e.Name())
		}
	}
	sort.Strings(upFiles)
	for _, name := range upFiles {
		sqlBytes, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
			return err
		}
	}
	return nil
}
