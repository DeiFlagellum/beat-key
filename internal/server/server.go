// Package server wystawia dwa publiczne endpointy serwera klucza:
// /info (klucz publiczny) i /share/<beat_index> (uwolniony udzial).
//
// SEAL.md 13, R5: przez ten serwer nie przechodza zadne dane uzytkownikow.
// Nie przyjmuje ciphertextow, nie ma POST-ow, nie ma sesji ani ciasteczek.
// Publikuje podpisy liczb.
package server

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"live.beattime/beat-key/internal/beat"
	"live.beattime/beat-key/internal/keystore"

	"github.com/drand/kyber/sign"
)

// SchemeID musi byc IDENTYCZNY ze schematem lancucha drand, z ktorym koperta
// sie komponuje (SEAL.md 6.2). Podpisy na G1, klucze na G2, hash-to-curve
// RFC 9380 z DST "BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_NUL_" — to samo, co
// daje domyslnie kyber-bls12381.
const SchemeID = "bls-unchained-g1-rfc9380"

// Server obsluguje zadania. Nie ma stanu poza kluczem i zegarem.
type Server struct {
	op     string
	store  *keystore.Store
	scheme sign.AggregatableScheme
	log    *slog.Logger

	// now jest wstrzykiwalne, zeby test mogl sprawdzic granice czasu bez
	// czekania 86 sekund.
	now func() time.Time
}

// New sklada serwer. `op` jest juz zwalidowany przez wywolujacego.
func New(op string, store *keystore.Store, scheme sign.AggregatableScheme, log *slog.Logger) *Server {
	return &Server{op: op, store: store, scheme: scheme, log: log, now: time.Now}
}

// SetClock podmienia zegar (tylko testy).
func (s *Server) SetClock(now func() time.Time) { s.now = now }

// Handler zwraca gotowy mux z nagliwkami CORS.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/info", s.handleInfo)
	mux.HandleFunc("/share/", s.handleShare)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})
	return s.withCORS(mux)
}

// PublicKeyB64 zwraca klucz publiczny w postaci, w jakiej trafia do koperty
// (pole `pub`, SEAL.md 8).
func (s *Server) PublicKeyB64() (string, error) {
	raw, err := s.store.Public().MarshalBinary()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

type infoResponse struct {
	Op             string `json:"op"`
	Scheme         string `json:"scheme"`
	PublicKey      string `json:"public_key"`
	IdentityFormat string `json:"identity_format"`
	BeatsPerDay    int64  `json:"beats_per_day"`
	BeatIndex      int64  `json:"beat_index"`
	BeatOfDay      int64  `json:"beat_of_day"`
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.fail(w, http.StatusMethodNotAllowed, "tylko GET")
		return
	}
	pub, err := s.PublicKeyB64()
	if err != nil {
		s.log.Error("serializacja klucza publicznego", "err", err)
		s.fail(w, http.StatusInternalServerError, "klucz publiczny niedostepny")
		return
	}
	idx := beat.IndexAt(s.now())
	// Klucz publiczny sie nie zmienia; biezacy beat zmienia sie co 86.4 s.
	w.Header().Set("Cache-Control", "public, max-age=60")
	s.writeJSON(w, http.StatusOK, infoResponse{
		Op:             s.op,
		Scheme:         SchemeID,
		PublicKey:      pub,
		IdentityFormat: beat.IdentityPrefix + "<beat_index>",
		BeatsPerDay:    beat.BeatsPerDay,
		BeatIndex:      idx,
		BeatOfDay:      beat.OfDay(idx),
	})
}

type shareResponse struct {
	Op        string `json:"op"`
	Scheme    string `json:"scheme"`
	BeatIndex int64  `json:"beat_index"`
	Identity  string `json:"identity"`
	Signature string `json:"signature"`
}

func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.fail(w, http.StatusMethodNotAllowed, "tylko GET")
		return
	}
	raw := strings.TrimPrefix(r.URL.Path, "/share/")
	if raw == "" || strings.Contains(raw, "/") {
		s.fail(w, http.StatusBadRequest, "sciezka to /share/<beat_index>")
		return
	}
	index, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "beat_index musi byc liczba calkowita")
		return
	}
	if index < 0 {
		s.fail(w, http.StatusBadRequest, "beat_index nie moze byc ujemny")
		return
	}

	// R4: udzial dla przyszlego beatu NIE ISTNIEJE. Nie "jeszcze nie", nie
	// czesciowa odpowiedz, nie podpowiedz ile zostalo — 404. Kazda inna
	// odpowiedz zaczyna byc kanalem informacyjnym o kluczu.
	if index > beat.IndexAt(s.now()) {
		s.fail(w, http.StatusNotFound, "ten beat jeszcze nie nadszedl")
		return
	}

	identity := beat.Identity(index)
	sig, err := s.store.Sign(s.scheme, identity)
	if err != nil {
		s.log.Error("podpisywanie tozsamosci", "beat_index", index, "err", err)
		s.fail(w, http.StatusInternalServerError, "podpis niedostepny")
		return
	}

	// Uwolniony udzial jest NIEZMIENNY i publiczny — im wiecej posrednikow
	// go zbuforuje i zlustruje, tym lepiej. To jest wlasnie ta wlasnosc, ktora
	// wyjmuje operatora ze sciezki dostepnosci (SEAL.md 7).
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	s.writeJSON(w, http.StatusOK, shareResponse{
		Op:        s.op,
		Scheme:    SchemeID,
		BeatIndex: index,
		Identity:  hex.EncodeToString(identity),
		Signature: base64.StdEncoding.EncodeToString(sig),
	})
}

// withCORS pozwala odpytac serwer z przegladarki. Wystawiamy wylacznie dane
// publiczne, wiec dowolne zrodlo jest w porzadku — bez tego klient webowy nie
// moglby otworzyc koperty.
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.log.Error("zapis odpowiedzi", "err", err)
	}
}

func (s *Server) fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
