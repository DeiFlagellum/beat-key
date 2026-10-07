package server

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// OperatorInfo — dane operatora w stopce strony pod `/` (opcjonalne, od 1.4.0).
//
// Strona serwera bywa jedynym miejscem, w ktorym ktos z koperta w reku sprawdza,
// KTO trzyma udzial. Identyfikator `op` mowi tylko tyle, ze to ten sam podmiot co
// w rejestrze; nazwa, adres i kontakt mowia, kim on jest. To oswiadczenie
// operatora, jak Impressum — serwer niczego tu nie weryfikuje, tylko pokazuje.
//
// Pola ida WYLACZNIE na strone HTML. /info zostaje bez zmian: to czesc protokolu
// (PROTOCOL.md), a dane kontaktowe nie sa potrzebne do sprawdzenia udzialu.
type OperatorInfo struct {
	Name     string // nazwa podmiotu (wymagana, gdy cokolwiek jest ustawione)
	Address  string // adres w jednej linii
	Registry string // np. numer w rejestrze albo NIP/VAT
	Contact  string // adres e-mail albo zwykly tekst
	URL      string // strona podmiotu, tylko https://
}

// ContactMail mowi, czy kontakt jest adresem e-mail (wtedy staje sie linkiem mailto:).
func (o OperatorInfo) ContactMail() bool {
	return emailPattern.MatchString(o.Contact)
}

// Nazwy zmiennych srodowiska — jedno zrodlo dla main.go, testow i dokumentacji.
const (
	EnvOperatorName     = "BEAT_KEY_OPERATOR_NAME"
	EnvOperatorAddress  = "BEAT_KEY_OPERATOR_ADDRESS"
	EnvOperatorRegistry = "BEAT_KEY_OPERATOR_REGISTRY"
	EnvOperatorContact  = "BEAT_KEY_OPERATOR_CONTACT"
	EnvOperatorURL      = "BEAT_KEY_OPERATOR_URL"
)

// MaxOperatorField — limit znakow (nie bajtow) na pole. Stopka to jedna linijka,
// a nie miejsce na regulamin.
const MaxOperatorField = 200

var emailPattern = regexp.MustCompile(`^[^@\s<>"]+@[^@\s<>"]+\.[^@\s<>"]+$`)

// OperatorInfoFromEnv czyta BEAT_KEY_OPERATOR_* przez `getenv` (os.Getenv albo
// atrapa w testach). Niepoprawne pola NIE blokuja startu: stopka to informacja,
// a nie warunek dzialania serwera klucza — zatrzymanie go z powodu literowki w
// adresie odebraloby ludziom udzialy. Takie pola sa pomijane i zwracane w `bad`,
// zeby main.go mogl je zalogowac.
//
// Bez nazwy nie ma stopki: kontakt bez podmiotu nic nie mowi o tym, kto trzyma
// udzial.
func OperatorInfoFromEnv(getenv func(string) string) (info OperatorInfo, bad []string) {
	field := func(key string) string {
		v := strings.TrimSpace(getenv(key))
		if v == "" {
			return ""
		}
		if utf8.RuneCountInString(v) > MaxOperatorField || hasControl(v) {
			bad = append(bad, key)
			return ""
		}
		return v
	}
	info.Name = field(EnvOperatorName)
	info.Address = field(EnvOperatorAddress)
	info.Registry = field(EnvOperatorRegistry)
	info.Contact = field(EnvOperatorContact)
	info.URL = field(EnvOperatorURL)

	if info.Contact != "" && strings.Contains(info.Contact, "@") && !emailPattern.MatchString(info.Contact) {
		bad = append(bad, EnvOperatorContact)
		info.Contact = ""
	}
	if info.URL != "" {
		u, err := url.Parse(info.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			bad = append(bad, EnvOperatorURL)
			info.URL = ""
		}
	}
	if info.Name == "" {
		if info.Address != "" || info.Registry != "" || info.Contact != "" || info.URL != "" {
			bad = append(bad, EnvOperatorName)
		}
		return OperatorInfo{}, bad
	}
	return info, bad
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// SetOperatorInfo ustawia dane operatora do stopki strony. Pusta nazwa = brak stopki.
func (s *Server) SetOperatorInfo(info OperatorInfo) {
	if info.Name == "" {
		s.operator = nil
		return
	}
	s.operator = &info
}
