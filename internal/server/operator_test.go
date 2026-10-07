package server

import (
	"strings"
	"testing"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// Stopka z danymi operatora: nazwa, adres, rejestr, e-mail jako mailto: i strona https.
func TestRootPageShowsOperatorDetailsWhenSet(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.SetOperatorInfo(OperatorInfo{
		Name:     "Przykład Sp. z o.o.",
		Address:  "ul. Testowa 1, 00-001 Warszawa, Polska",
		Registry: "NIP 0000000000",
		Contact:  "kontakt@example.org",
		URL:      "https://example.org/",
	})
	body := get(t, srv, "/").Body.String()
	for _, needle := range []string{
		"<strong>Operator:</strong> Przykład Sp. z o.o.",
		"ul. Testowa 1, 00-001 Warszawa, Polska",
		"NIP 0000000000",
		`href="mailto:kontakt@example.org"`,
		`href="https://example.org/"`,
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("brak %q w stopce", needle)
		}
	}
}

// Bez danych — strona jak dotad, bez linijki "Operator:".
func TestRootPageWithoutOperatorDetails(t *testing.T) {
	srv, _, _ := newTestServer(t)
	if body := get(t, srv, "/").Body.String(); strings.Contains(body, "Operator:") {
		t.Fatal("stopka z danymi operatora pojawila sie bez danych")
	}
}

// Dane podaje operator — i tak sa tylko tekstem: html/template je koduje.
func TestOperatorDetailsAreEscaped(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.SetOperatorInfo(OperatorInfo{Name: `<script>alert(1)</script>`, Contact: `"><b>x</b>`})
	body := get(t, srv, "/").Body.String()
	if strings.Contains(body, "<script>alert(1)") || strings.Contains(body, "<b>x</b>") {
		t.Fatal("dane operatora trafily na strone jako HTML")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("brak zakodowanej nazwy")
	}
}

// Zle pola sa pomijane (i zglaszane), dobre zostaja; start serwera nie zalezy od stopki.
func TestOperatorInfoFromEnvSkipsBadFields(t *testing.T) {
	info, bad := OperatorInfoFromEnv(envMap(map[string]string{
		EnvOperatorName:     "  Konto Testowe  ",
		EnvOperatorAddress:  "linia 1\nlinia 2",
		EnvOperatorRegistry: strings.Repeat("x", MaxOperatorField+1),
		EnvOperatorContact:  "x@y",
		EnvOperatorURL:      "http://bez-https.example",
	}))
	if info.Name != "Konto Testowe" {
		t.Errorf("Name = %q, oczekiwano przycietej nazwy", info.Name)
	}
	if info.Address != "" || info.Registry != "" || info.Contact != "" || info.URL != "" {
		t.Errorf("zle pola nie zostaly pominiete: %+v", info)
	}
	for _, key := range []string{EnvOperatorAddress, EnvOperatorRegistry, EnvOperatorContact, EnvOperatorURL} {
		if !contains(bad, key) {
			t.Errorf("%s nie zgloszone jako niepoprawne (bad=%v)", key, bad)
		}
	}
}

// Kontakt bez nazwy podmiotu nic nie mowi o tym, kto trzyma udzial — brak stopki.
func TestOperatorInfoNeedsAName(t *testing.T) {
	info, bad := OperatorInfoFromEnv(envMap(map[string]string{EnvOperatorContact: "kontakt@example.org"}))
	if info.Name != "" || info.Contact != "" {
		t.Errorf("bez nazwy nie powinno byc danych: %+v", info)
	}
	if !contains(bad, EnvOperatorName) {
		t.Errorf("brak nazwy nie zgloszony (bad=%v)", bad)
	}
	if info, bad := OperatorInfoFromEnv(envMap(nil)); info.Name != "" || len(bad) != 0 {
		t.Errorf("bez zmiennych: %+v %v — oczekiwano pustych", info, bad)
	}
}

// Kontakt bez "@" to zwykly tekst (np. telefon), bez linku mailto:.
func TestPlainTextContactIsNotAMailLink(t *testing.T) {
	info, bad := OperatorInfoFromEnv(envMap(map[string]string{
		EnvOperatorName:    "Firma",
		EnvOperatorContact: "+48 12 000 00 00",
	}))
	if len(bad) != 0 || info.Contact != "+48 12 000 00 00" || info.ContactMail() {
		t.Fatalf("kontakt tekstowy: %+v bad=%v mail=%v", info, bad, info.ContactMail())
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
