// Package beat liczy absolutny indeks beatu i tozsamosc IBE dla niego.
//
// KONTRAKT wspolny z apps/beat/core.py i apps/tsa/SEAL.md sekcja 3: beat
// liczymy na CALKOWITYCH mikrosekundach uniksowych, nigdy przez dzielenie
// przez 86.4 (86.4 nie jest przedstawialne binarnie w double, a floor spada
// wtedy o jeden na granicach).
package beat

import (
	"crypto/sha256"
	"strconv"
	"time"
)

const (
	// MicrosecondsPerBeat to 86.4 s wyrazone w mikrosekundach.
	MicrosecondsPerBeat = 86_400_000
	// BeatsPerDay — doba ma 1000 beatow.
	BeatsPerDay = 1000

	// IdentityPrefix to KONTRAKT tozsamosci IBE (SEAL.md 6.2). Zmiana tego
	// napisu uniewaznia KAZDA istniejaca koperte — nowy prefiks wersji,
	// nigdy zmiana w miejscu.
	IdentityPrefix = "beattime-beat-v1|"
)

// IndexAt zwraca absolutny indeks beatu dla chwili t.
//
// Indeks 0 to 1970-01-01T00:00:00Z (czyli @000). Kotwica w UTC, nie BMT.
func IndexAt(t time.Time) int64 {
	return floorDiv(t.UTC().UnixMicro(), MicrosecondsPerBeat)
}

// Identity zwraca bajty tozsamosci IBE dla indeksu (SEAL.md 6.2):
// SHA-256("beattime-beat-v1|" + dziesietny indeks), ASCII, bez paddingu.
func Identity(index int64) []byte {
	sum := sha256.Sum256([]byte(IdentityPrefix + strconv.FormatInt(index, 10)))
	return sum[:]
}

// OfDay zwraca beat doby (0..999) — to, co pokazujemy jako '@NNN'.
func OfDay(index int64) int64 {
	d := index % BeatsPerDay
	if d < 0 {
		d += BeatsPerDay
	}
	return d
}

// floorDiv dzieli W DOL takze dla ujemnych. Go tnie w strone zera, Python
// dzieli w dol — a kontrakt musi dawac ten sam indeks po obu stronach.
// Indeksy ujemne (przed 1970) nie sa podpisywane, ale rozjazd arytmetyki
// miedzy implementacjami to dokladnie ten rodzaj bledu, ktory ujawnia sie
// dopiero po latach.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}
