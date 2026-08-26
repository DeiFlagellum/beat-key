// Command verify-share sprawdza uwolniony udzial wobec klucza publicznego
// operatora — lokalnie, bez pytania kogokolwiek o zdanie.
//
// To jest narzedzie, ktore czyni realna obietnice z SEAL.md sekcja 7: udzial
// pobrany z DOWOLNEGO zrodla (rejestru, mirrora, archiwum, pendrajwa od kogos
// obcego) weryfikuje sie wobec `pub` z koperty. Operator nie jest ani w
// sciezce zaufania, ani w sciezce dostepnosci.
//
// Uzycie:
//
//	verify-share -pub <base64> -beat <beat_index> -sig <base64>
//
// Kod wyjscia 0 = podpis prawidlowy, 1 = nieprawidlowy lub blad.
package main

import (
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"live.beattime/beat-key/internal/beat"

	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/sign/bls"
)

func main() {
	pubB64 := flag.String("pub", "", "klucz publiczny operatora (base64, pole `pub` z koperty)")
	sigB64 := flag.String("sig", "", "podpis z /share/<beat_index> (base64)")
	index := flag.Int64("beat", -1, "beat_index, ktorego dotyczy udzial")
	flag.Parse()

	if *pubB64 == "" || *sigB64 == "" || *index < 0 {
		flag.Usage()
		os.Exit(1)
	}

	suite := bls12381.NewBLS12381Suite()
	scheme := bls.NewSchemeOnG1(suite)

	pubRaw, err := base64.StdEncoding.DecodeString(*pubB64)
	if err != nil {
		fail("klucz publiczny nie jest base64: %v", err)
	}
	public := suite.G2().Point()
	if err := public.UnmarshalBinary(pubRaw); err != nil {
		fail("klucz publiczny nie jest punktem G2: %v", err)
	}

	sig, err := base64.StdEncoding.DecodeString(*sigB64)
	if err != nil {
		fail("podpis nie jest base64: %v", err)
	}

	// Tozsamosc liczymy SAMI z indeksu — nie przyjmujemy jej od nikogo.
	// Gdyby serwer podal inna tozsamosc niz wynika z kontraktu, podpis
	// moglby sie zgadzac, a udzial i tak nie otworzylby koperty.
	identity := beat.Identity(*index)

	if err := scheme.Verify(public, identity, sig); err != nil {
		fail("PODPIS NIEPRAWIDLOWY dla beat_index=%d: %v", *index, err)
	}

	fmt.Printf("OK  beat_index=%d  @%03d  identity=%s\n",
		*index, beat.OfDay(*index), hex.EncodeToString(identity))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
