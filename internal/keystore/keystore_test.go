package keystore

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/pairing"
	"github.com/drand/kyber/sign"
	"github.com/drand/kyber/sign/bls"
)

func newScheme() (pairing.Suite, sign.AggregatableScheme) {
	suite := bls12381.NewBLS12381Suite()
	return suite, bls.NewSchemeOnG1(suite)
}

// R1: pusty wolumen = nowy klucz, wytworzony na miejscu.
func TestGeneratesKeyOnFirstStart(t *testing.T) {
	dir := t.TempDir()
	suite, scheme := newScheme()

	store, err := Open(dir, suite, scheme)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !store.Created {
		t.Fatal("pierwszy start nie zglosil wytworzenia klucza")
	}
	if _, err := os.Stat(filepath.Join(dir, KeyFile)); err != nil {
		t.Fatalf("klucz nie zostal zapisany: %v", err)
	}
}

// Drugi start MUSI wziac ten sam klucz. Gdyby generowal nowy, kazda koperta
// zapieczetowana wobec starego stalaby sie nieotwieralna.
func TestKeyPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	suite, scheme := newScheme()

	first, err := Open(dir, suite, scheme)
	if err != nil {
		t.Fatalf("pierwszy Open: %v", err)
	}
	second, err := Open(dir, suite, scheme)
	if err != nil {
		t.Fatalf("drugi Open: %v", err)
	}
	if second.Created {
		t.Error("drugi start zglosil wytworzenie klucza — nadpisal istniejacy")
	}
	if !first.Public().Equal(second.Public()) {
		t.Fatal("klucz publiczny zmienil sie miedzy startami")
	}
}

// Uszkodzony klucz to odmowa startu, NIE cicha regeneracja. Nowy klucz w tym
// miejscu uniewaznilby wszystkie istniejace koperty.
func TestCorruptKeyRefusesToStart(t *testing.T) {
	dir := t.TempDir()
	suite, scheme := newScheme()
	if _, err := Open(dir, suite, scheme); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, KeyFile), []byte("nie-klucz"), 0o600); err != nil {
		t.Fatalf("psucie klucza: %v", err)
	}
	if _, err := Open(dir, suite, scheme); err == nil {
		t.Fatal("uszkodzony klucz nie zablokowal startu")
	}
}

// R2: plik klucza nie moze byc czytelny dla nikogo poza wlascicielem.
func TestKeyFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tryby POSIX nie maja zastosowania na Windows; kontener jest linuksowy")
	}
	dir := t.TempDir()
	suite, scheme := newScheme()
	if _, err := Open(dir, suite, scheme); err != nil {
		t.Fatalf("Open: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, KeyFile))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Fatalf("tryb pliku klucza %04o — czytelny poza wlascicielem", mode)
	}
}

// Dwa niezalezne katalogi = dwa niezalezne klucze. Gdyby ktos wdrozyl obraz u
// trzech partnerow, kazdy MUSI dostac inny klucz.
func TestSeparateVolumesGetSeparateKeys(t *testing.T) {
	suite, scheme := newScheme()
	a, err := Open(t.TempDir(), suite, scheme)
	if err != nil {
		t.Fatalf("Open a: %v", err)
	}
	b, err := Open(t.TempDir(), suite, scheme)
	if err != nil {
		t.Fatalf("Open b: %v", err)
	}
	if a.Public().Equal(b.Public()) {
		t.Fatal("dwa osobne wolumeny dostaly ten sam klucz")
	}
}

// Exists nie moze niczego tworzyc — na tym stoi bezpiecznik
// BEAT_KEY_EXPECT_PUB, ktory ma odmowic startu ZANIM powstanie jakikolwiek
// plik w zle podpietym woluminie.
func TestExistsDoesNotCreateAnything(t *testing.T) {
	dir := t.TempDir()
	if Exists(dir) {
		t.Fatal("Exists zglosil klucz w pustym katalogu")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Exists zostawil po sobie %d plikow", len(entries))
	}

	suite, scheme := newScheme()
	if _, err := Open(dir, suite, scheme); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !Exists(dir) {
		t.Fatal("Exists nie widzi wytworzonego klucza")
	}
}
