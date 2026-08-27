package server

import (
	"html/template"
	"net/http"

	"live.beattime/beat-key/internal/beat"
)

// Strona pod `/`.
//
// Nie jest ozdoba. Adres tego serwera trafia do kopert jako podpowiedz, gdzie
// szukac udzialu — i zostaje tam na lata. Ktos, kto otwiera koperte w 2036
// roku, moze go po prostu wkleic w przegladarke. "404 page not found" jest
// wtedy slepym zaulkiem; ta strona mowi, czym ten serwer jest, kto go prowadzi
// i gdzie jest opis protokolu.
//
// Wszystko inline: zaden zewnetrzny plik, zadna czcionka z sieci. Obraz ma
// zostac maly, a strona ma dzialac takze wtedy, gdy internet wokol niej juz
// wyglada inaczej niz dzis.
var rootTmpl = template.Must(template.New("root").Parse(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex">
<title>beat-key — {{.Op}}</title>
<style>
:root{--bg:#f4f5f1;--fg:#1e2422;--dim:#5a615b;--line:#d3d6cd;--card:#fbfcf9;--acc:#8a6420}
@media(prefers-color-scheme:dark){:root{--bg:#121716;--fg:#e3e6e0;--dim:#98a099;--line:#2b3331;--card:#1a201e;--acc:#d6a95b}}
*{box-sizing:border-box}
body{margin:0;padding:0 1.2rem 4rem;background:var(--bg);color:var(--fg);
 font:16px/1.6 system-ui,-apple-system,"Segoe UI",sans-serif}
main{max-width:40rem;margin:0 auto}
h1{font-size:1.6rem;font-weight:600;letter-spacing:-.02em;margin:3rem 0 .3rem}
h2{font-size:1rem;font-weight:600;margin:2.2rem 0 .5rem}
p,li{color:var(--dim)}
ul{padding-inline-start:1.2rem}
a{color:var(--acc)}
code,.m{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.86em}
.sub{color:var(--dim);margin:0 0 1.6rem}
.box{background:var(--card);border:1px solid var(--line);border-radius:6px;
 padding:.9rem 1.1rem;margin:1rem 0}
.row{display:flex;gap:.6rem;padding:.3rem 0;flex-wrap:wrap}
.row .k{color:var(--dim);min-width:9rem;font-size:.85rem}
.row .v{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.82rem;
 word-break:break-all;flex:1;min-width:0}
.note{border-left:3px solid var(--acc);padding-left:.9rem;margin:1.4rem 0}
footer{margin-top:2.5rem;padding-top:1.2rem;border-top:1px solid var(--line);
 font-size:.85rem;color:var(--dim)}
</style></head><body><main>

<h1>beat-key</h1>
<p class="sub">A key server for time-sealed envelopes, operated by
<strong>{{.Op}}</strong>.</p>

<p>This machine holds <strong>one share</strong> of a split key. It publishes a
signature once a given moment has passed, and nothing else. On its own that
share opens nothing — an envelope needs several, held by parties that do not
answer to each other.</p>

<div class="box">
  <div class="row"><span class="k">operator</span><span class="v">{{.Op}}</span></div>
  <div class="row"><span class="k">scheme</span><span class="v">{{.Scheme}}</span></div>
  <div class="row"><span class="k">public key</span><span class="v">{{.PublicKey}}</span></div>
  <div class="row"><span class="k">current beat</span><span class="v">{{.BeatIndex}} · @{{printf "%03d" .BeatOfDay}}</span></div>
</div>

<h2>Endpoints</h2>
<ul>
  <li><a href="/info"><code>/info</code></a> — this operator's public key and scheme</li>
  <li><code>/share/&lt;beat_index&gt;</code> — the released share for that moment.
      <strong>404 before it arrives</strong>, never a hint about how long is left</li>
  <li><a href="/healthz"><code>/healthz</code></a> — liveness</li>
</ul>

<div class="note">
<p>A share fetched from here is verified <strong>against the public key recorded
in the envelope</strong>, not against this server. Once published it is ordinary
public data: copy it, mirror it, archive it. This host is in neither the trust
path nor the availability path.</p>
</div>

<h2>What this server does not do</h2>
<ul>
  <li>It cannot open an envelope, and neither can anyone else holding a single share.</li>
  <li>It stores no user data. No ciphertext passes through it — it publishes
      signatures of numbers.</li>
  <li>It keeps no record of who asked for what.</li>
</ul>

<h2>Found this address in an envelope?</h2>
<p>Then you are in the right place, and you probably want
<a href="https://github.com/DeiFlagellum/beat-key/blob/main/PROTOCOL.md">the protocol</a>
— it describes exactly what a share is and how to check one. The envelope format
itself is documented at
<a href="https://beattime.live/seal/">beattime.live/seal</a>.</p>

<footer>
Source, reproducible builds and the operator guide:
<a href="https://github.com/DeiFlagellum/beat-key">github.com/DeiFlagellum/beat-key</a>.
The image is public so that any operator can rebuild it and compare.
</footer>

</main></body></html>
`))

type rootData struct {
	Op        string
	Scheme    string
	PublicKey string
	BeatIndex int64
	BeatOfDay int64
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	// ServeMux kieruje tu KAZDA nieznana sciezke, nie tylko "/". Bez tego
	// sprawdzenia literowka w adresie udzialu dostawalaby strone powitalna
	// zamiast 404 i wygladalaby na sukces.
	if r.URL.Path != "/" {
		s.fail(w, http.StatusNotFound, "nie ma takiej sciezki; zobacz / albo /info")
		return
	}
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

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	if err := rootTmpl.Execute(w, rootData{
		Op:        s.op,
		Scheme:    SchemeID,
		PublicKey: pub,
		BeatIndex: idx,
		BeatOfDay: beat.OfDay(idx),
	}); err != nil {
		s.log.Error("render strony glownej", "err", err)
	}
}
