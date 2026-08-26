// Command beat-key to serwer klucza operatora dla kopert beattime-seal-v1.
//
// Uwalnia udzial `beat` — podpis BLS tozsamosci danego beatu — dopiero gdy ten
// beat nadejdzie. Pelny kontrakt: apps/tsa/SEAL.md, sekcje 6.2, 7 i 13.
//
// Serwer NIE przyjmuje zadnych danych uzytkownikow. Nie ma POST-ow, nie ma
// bazy, nie ma logow dostepu z trescia. Publikuje podpisy liczb.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"live.beattime/beat-key/internal/beat"
	"live.beattime/beat-key/internal/keystore"
	"live.beattime/beat-key/internal/server"

	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/sign/bls"
)

// opPattern — SEAL.md 7: identyfikator PODMIOTU, nadawany raz i nigdy nie
// zmieniany. Bez wielkich liter, bez kropek, bez nazw srodowisk.
var opPattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	op := os.Getenv("BEAT_KEY_OP")
	if !opPattern.MatchString(op) {
		log.Error("BEAT_KEY_OP musi pasowac do ^[a-z0-9-]{1,32}$ " +
			"i identyfikowac PODMIOT, nie srodowisko (zadnych -prod, -vps2)")
		os.Exit(2)
	}
	dir := envOr("BEAT_KEY_DIR", "/data")
	addr := envOr("BEAT_KEY_ADDR", ":8080")

	suite := bls12381.NewBLS12381Suite()
	// Podpisy na G1, klucze na G2 — parzystosc ze schematem drand
	// bls-unchained-g1-rfc9380 (SEAL.md 6.2).
	scheme := bls.NewSchemeOnG1(suite)

	store, err := keystore.Open(dir, suite, scheme)
	if err != nil {
		log.Error("klucz operatora", "err", err)
		os.Exit(1)
	}

	srv := server.New(op, store, scheme, log)
	pub, err := srv.PublicKeyB64()
	if err != nil {
		log.Error("klucz publiczny", "err", err)
		os.Exit(1)
	}

	if store.Created {
		// Ten komunikat jest jedynym momentem, w ktorym operator dowiaduje
		// sie, ze klucz jest JEGO. Klucz prywatny nie pojawia sie tu ani
		// nigdzie indziej w logu.
		log.Info("wygenerowano NOWY klucz operatora — od tej chwili plik "+
			keystore.KeyFile+" w woluminie danych jest jedyna jego kopia",
			"op", op, "katalog", dir)
	}
	log.Info("beat-key gotowy",
		"op", op, "adres", addr, "scheme", server.SchemeID,
		"public_key", pub, "beat_index", beat.IndexAt(time.Now()))

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serwer HTTP", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("zatrzymywanie")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("zamykanie serwera", "err", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
