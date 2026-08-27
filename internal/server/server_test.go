package server

import (
	"encoding/base64"
	"html"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"live.beattime/beat-key/internal/beat"
	"live.beattime/beat-key/internal/keystore"

	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/sign"
	"github.com/drand/kyber/sign/bls"

	"log/slog"
)

const testBeat = int64(20691500) // 2026-08-26T12:00:00Z, czyli @500

func newTestServer(t *testing.T) (*Server, sign.AggregatableScheme, *keystore.Store) {
	t.Helper()
	suite := bls12381.NewBLS12381Suite()
	scheme := bls.NewSchemeOnG1(suite)
	store, err := keystore.Open(t.TempDir(), suite, scheme)
	if err != nil {
		t.Fatalf("keystore: %v", err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := New("test-operator", store, scheme, log)
	// Zegar zatrzymany dokladnie na testBeat.
	srv.SetClock(func() time.Time { return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC) })
	return srv, scheme, store
}

func get(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// R4: udzial dla PRZYSZLEGO beatu nie istnieje. Nie "jeszcze nie", nie 425,
// nie czesciowa odpowiedz — 404.
func TestFutureBeatIs404(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, idx := range []int64{testBeat + 1, testBeat + 1000, testBeat + 1_000_000} {
		rec := get(t, srv, "/share/"+itoa(idx))
		if rec.Code != http.StatusNotFound {
			t.Errorf("/share/%d dalo %d, oczekiwano 404", idx, rec.Code)
		}
		if body := rec.Body.String(); containsAny(body, "signature", "identity") {
			t.Errorf("/share/%d wyciekl fragment odpowiedzi: %s", idx, body)
		}
	}
}

// Biezacy beat JEST juz uwolniony — granica jest domknieta od dolu.
func TestCurrentBeatIsReleased(t *testing.T) {
	srv, _, _ := newTestServer(t)
	if rec := get(t, srv, "/share/"+itoa(testBeat)); rec.Code != http.StatusOK {
		t.Fatalf("biezacy beat dal %d, oczekiwano 200", rec.Code)
	}
}

// Uwolniony udzial musi weryfikowac sie wobec klucza publicznego z /info —
// to jest cala umowa z posiadaczem koperty.
func TestReleasedShareVerifiesAgainstPublicKey(t *testing.T) {
	srv, scheme, store := newTestServer(t)

	rec := get(t, srv, "/share/"+itoa(testBeat-10))
	if rec.Code != http.StatusOK {
		t.Fatalf("kod %d", rec.Code)
	}
	var share shareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &share); err != nil {
		t.Fatalf("json: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(share.Signature)
	if err != nil {
		t.Fatalf("base64 podpisu: %v", err)
	}
	identity := beat.Identity(testBeat - 10)
	if err := scheme.Verify(store.Public(), identity, sig); err != nil {
		t.Fatalf("podpis nie weryfikuje sie wobec klucza publicznego: %v", err)
	}
	if share.Scheme != SchemeID {
		t.Errorf("scheme = %q, oczekiwano %q", share.Scheme, SchemeID)
	}
}

// Podpis BLS jest deterministyczny — to warunek konieczny, zeby udzial dalo
// sie lustrowac i buforowac (SEAL.md 7).
func TestSignatureIsDeterministic(t *testing.T) {
	srv, _, _ := newTestServer(t)
	first := get(t, srv, "/share/"+itoa(testBeat-1)).Body.String()
	second := get(t, srv, "/share/"+itoa(testBeat-1)).Body.String()
	if first != second {
		t.Fatalf("dwa zadania daly rozne odpowiedzi:\n%s\n%s", first, second)
	}
}

func TestInfoExposesPublicKeyOnly(t *testing.T) {
	srv, _, _ := newTestServer(t)
	rec := get(t, srv, "/info")
	if rec.Code != http.StatusOK {
		t.Fatalf("kod %d", rec.Code)
	}
	var info infoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("json: %v", err)
	}
	if info.Op != "test-operator" || info.Scheme != SchemeID {
		t.Errorf("op/scheme: %+v", info)
	}
	if info.BeatIndex != testBeat || info.BeatOfDay != 500 {
		t.Errorf("beat: %d/@%03d, oczekiwano %d/@500", info.BeatIndex, info.BeatOfDay, testBeat)
	}
	if _, err := base64.StdEncoding.DecodeString(info.PublicKey); err != nil {
		t.Errorf("public_key nie jest base64: %v", err)
	}
	if info.IdentityFormat != beat.IdentityPrefix+"<beat_index>" {
		t.Errorf("identity_format = %q", info.IdentityFormat)
	}
}

func TestBadInputIs400(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, path := range []string{"/share/", "/share/abc", "/share/-1", "/share/1/2", "/share/1e9"} {
		if rec := get(t, srv, path); rec.Code != http.StatusBadRequest {
			t.Errorf("%s dalo %d, oczekiwano 400", path, rec.Code)
		}
	}
}

// R5: serwer nie przyjmuje danych. Zaden POST nie moze byc obsluzony.
func TestWriteMethodsRejected(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(method, "/share/"+itoa(testBeat), nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s dal %d, oczekiwano 405", method, rec.Code)
		}
	}
}

// Uwolniony udzial jest niezmienny — buforowalnosc jest czescia projektu,
// nie optymalizacja.
func TestReleasedShareIsCacheableForever(t *testing.T) {
	srv, _, _ := newTestServer(t)
	rec := get(t, srv, "/share/"+itoa(testBeat-5))
	if cc := rec.Header().Get("Cache-Control"); !containsAny(cc, "immutable") {
		t.Fatalf("Cache-Control = %q, oczekiwano immutable", cc)
	}
}

func itoa(v int64) string {
	if v < 0 {
		return "-" + itoa(-v)
	}
	if v < 10 {
		return string(rune('0' + v))
	}
	return itoa(v/10) + string(rune('0'+v%10))
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// Adres serwera trafia do kopert jako podpowiedz i zostaje tam na lata. Ktos,
// kto wklei go w przegladarke, ma zobaczyc, czym ten serwer jest — a nie golе
// "404 page not found".
func TestRootPageExplainsWhatThisIs(t *testing.T) {
	srv, _, _ := newTestServer(t)
	rec := get(t, srv, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("/ dalo %d, oczekiwano 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !containsAny(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, needle := range []string{"test-operator", SchemeID, "/share/", "PROTOCOL.md"} {
		if !containsAny(body, needle) {
			t.Errorf("brak %q na stronie glownej", needle)
		}
	}
}

// Kazda nieznana sciezka trafia do tego samego handlera. Literowka w adresie
// udzialu MUSI dac 404, a nie strone powitalna — inaczej wyglada na sukces.
func TestUnknownPathIsStill404(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, path := range []string{"/shares/123", "/info2", "/admin", "/.env"} {
		if rec := get(t, srv, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s dalo %d, oczekiwano 404", path, rec.Code)
		}
	}
}

// Klucz PRYWATNY nie moze wyciec zadnym kanalem, takze przez strone.
func TestRootPageLeaksNoPrivateKey(t *testing.T) {
	srv, scheme, store := newTestServer(t)
	body := get(t, srv, "/").Body.String()
	sig, err := store.Sign(scheme, []byte("proba"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if containsAny(body, string(sig)) {
		t.Fatal("strona zawiera material zwiazany z kluczem prywatnym")
	}
	// html/template koduje `+` jako `&#43;`, a base64 plusy zawiera — w
	// przegladarce wyswietla sie to poprawnie, wiec sprawdzamy tresc PO
	// odkodowaniu, a nie zrodlo doslownie. Postac maszynowa klucza i tak
	// wydaje /info, nie ta strona.
	pub, _ := srv.PublicKeyB64()
	if !containsAny(html.UnescapeString(body), pub) {
		t.Error("strona nie pokazuje klucza PUBLICZNEGO, a powinna")
	}
}
