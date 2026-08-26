package beat

import (
	"encoding/hex"
	"testing"
	"time"
)

// Wektory ZAMROZONE — policzone niezaleznie w Pythonie ta sama arytmetyka co
// apps/beat/core.py (calkowite mikrosekundy // 86_400_000). Jesli ktorykolwiek
// przestanie sie zgadzac, rozjechal sie kontrakt miedzy serwerem klucza a
// reszta BeatTime i KAZDA istniejaca koperta jest zagrozona.
var vectors = []struct {
	iso      string
	index    int64
	ofDay    int64
	identity string
}{
	{"1970-01-01T00:00:00Z", 0, 0,
		"fc6f25e7c706affd060eb3b4e0b91611df653ff9e6e46d75f0d3f2554c31f858"},
	{"1970-01-01T00:01:26.4Z", 1, 1,
		"acb3f0595ce40d1a54a4e06fd57c9eb8976bbf55e66c2580b80eba864906324a"},
	{"2026-08-26T12:00:00Z", 20691500, 500,
		"33c94069ff978bd1a9e9584181446d8e4297fea45e7a0a68242258d73987b388"},
	{"2027-03-14T09:12:00Z", 20891383, 383,
		"b15d64ee9cf967315f2fe97c4cfd3f8366d960d2fe96684025910d9ca8f8a5c1"},
}

func TestVectorsFrozen(t *testing.T) {
	for _, v := range vectors {
		ts, err := time.Parse(time.RFC3339Nano, v.iso)
		if err != nil {
			t.Fatalf("%s: %v", v.iso, err)
		}
		if got := IndexAt(ts); got != v.index {
			t.Errorf("IndexAt(%s) = %d, oczekiwano %d", v.iso, got, v.index)
		}
		if got := OfDay(v.index); got != v.ofDay {
			t.Errorf("OfDay(%d) = %d, oczekiwano %d", v.index, got, v.ofDay)
		}
		if got := hex.EncodeToString(Identity(v.index)); got != v.identity {
			t.Errorf("Identity(%d) = %s, oczekiwano %s", v.index, got, v.identity)
		}
	}
}

// Poludnie UTC to @500 — najprostszy mozliwy test kotwicy. Gdyby ktos kiedys
// przestawil kotwice na BMT (UTC+1, jak Swatch w 1998), ten test padnie.
func TestNoonIsBeat500(t *testing.T) {
	noon := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	if got := OfDay(IndexAt(noon)); got != 500 {
		t.Fatalf("poludnie UTC = @%03d, oczekiwano @500", got)
	}
}

// Granica beatu musi byc ostra: ostatnia mikrosekunda nalezy jeszcze do
// poprzedniego indeksu. To ten sam rodzaj bledu, ktory przy dzieleniu przez
// 86.4 dawal centibeat mniej.
func TestBeatBoundaryIsExact(t *testing.T) {
	base := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	justBefore := base.Add(86400*time.Microsecond*1000 - time.Microsecond)
	if got := IndexAt(justBefore); got != 0 {
		t.Errorf("mikrosekunde przed granica: %d, oczekiwano 0", got)
	}
	if got := IndexAt(base.Add(86400 * time.Microsecond * 1000)); got != 1 {
		t.Errorf("dokladnie na granicy: %d, oczekiwano 1", got)
	}
}

// Indeksy sprzed 1970 nie sa podpisywane, ale arytmetyka musi dzielic W DOL
// tak samo jak Python — inaczej implementacje rozjezdzaja sie cicho.
func TestNegativeIndexFloorsDown(t *testing.T) {
	before := time.Date(1969, 12, 31, 23, 59, 59, 0, time.UTC)
	if got := IndexAt(before); got != -1 {
		t.Fatalf("IndexAt tuz przed epoka = %d, oczekiwano -1", got)
	}
	if got := OfDay(-1); got != 999 {
		t.Fatalf("OfDay(-1) = %d, oczekiwano 999", got)
	}
}
