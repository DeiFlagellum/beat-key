// Package keystore trzyma klucz prywatny BLS operatora.
//
// SEAL.md 13, R1: klucz moze powstac WYLACZNIE tutaj, wewnatrz kontenera,
// przy pierwszym starcie. W calym programie nie ma sciezki importu klucza z
// zewnatrz — ani flagi, ani zmiennej srodowiskowej, ani endpointu. Dzieki
// temu nie da sie ani zapomniec o wygenerowaniu wlasnego klucza, ani
// przypadkiem wdrozyc cudzego.
//
// SEAL.md 13, R2: klucz prywatny nie opuszcza tego katalogu. Store nie
// wystawia zadnej metody, ktora zwracalaby go w formie serializowanej, i
// nigdy nie trafia do logu.
package keystore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/drand/kyber"
	"github.com/drand/kyber/pairing"
	"github.com/drand/kyber/sign"
	"github.com/drand/kyber/util/random"
)

// KeyFile to nazwa pliku klucza w katalogu danych.
const KeyFile = "key.bin"

// Store to para kluczy operatora wczytana z woluminu albo tam wytworzona.
type Store struct {
	private kyber.Scalar
	public  kyber.Point

	// Created mowi, czy klucz powstal przy TYM starcie. Uzywane tylko do
	// komunikatu startowego — operator musi zobaczyc, ze klucz jest jego.
	Created bool
}

// Open wczytuje klucz z dir albo generuje nowy, gdy zadnego tam nie ma.
func Open(dir string, suite pairing.Suite, scheme sign.AggregatableScheme) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("katalog danych %s: %w", dir, err)
	}
	path := filepath.Join(dir, KeyFile)

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		private := suite.G2().Scalar()
		if err := private.UnmarshalBinary(raw); err != nil {
			// Swiadomie NIE generujemy nowego klucza w tym miejscu. Nowy
			// klucz cicho uniewaznilby kazda koperte zapieczetowana wobec
			// starego — lepiej odmowic startu i pozwolic czlowiekowi
			// odzyskac plik z kopii.
			return nil, fmt.Errorf(
				"klucz %s jest uszkodzony (%w) — NIE usuwaj go, odtworz z kopii",
				path, err)
		}
		return &Store{
			private: private,
			public:  suite.G2().Point().Mul(private, nil),
		}, nil

	case errors.Is(err, os.ErrNotExist):
		private, public := scheme.NewKeyPair(random.New())
		blob, err := private.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("serializacja nowego klucza: %w", err)
		}
		if err := writeAtomic(path, blob); err != nil {
			return nil, fmt.Errorf("zapis nowego klucza: %w", err)
		}
		return &Store{private: private, public: public, Created: true}, nil

	default:
		return nil, fmt.Errorf("odczyt %s: %w", path, err)
	}
}

// Public zwraca klucz publiczny. Jedyna czesc pary, ktora wolno pokazac.
func (s *Store) Public() kyber.Point { return s.public }

// Sign podpisuje wiadomosc kluczem prywatnym. To JEDYNY sposob, w jaki klucz
// prywatny wplywa na cokolwiek poza pakietem.
func (s *Store) Sign(scheme sign.AggregatableScheme, msg []byte) ([]byte, error) {
	return scheme.Sign(s.private, msg)
}

// writeAtomic zapisuje plik przez tymczasowy + rename, zeby przerwany start
// nie zostawil obcietego klucza. Tryb 0600 od razu przy tworzeniu, nie po —
// inaczej istnieje okno, w ktorym klucz jest czytelny dla innych.
func writeAtomic(path string, blob []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".key-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op po udanym rename

	if err := tmp.Chmod(0o600); err != nil && !errors.Is(err, os.ErrInvalid) {
		// Windows nie zna trybow POSIX; na Linuksie (docelowa platforma
		// kontenera) blad jest istotny.
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(blob); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
