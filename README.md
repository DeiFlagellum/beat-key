# beat-key

A key server for **beattime-seal-v1** time-sealed envelopes.

It publishes one thing: a BLS signature over a beat identity — and only once
that beat has arrived. Combined with an independent beacon (drand), it makes an
envelope that **no single party can open early**, including the person who
built it.

Full protocol: [`PROTOCOL.md`](PROTOCOL.md).

---

## What you are agreeing to run

If someone asked you to operate this, here is the honest summary.

**What it does.** Every 86.4 seconds a new "beat" begins. On request, this
server signs the identity of any beat that has already passed. It refuses,
with a plain 404, for any beat still in the future.

**What it holds.** One 32-byte private key, generated on your machine, on first
start, inside the container. It never leaves the data volume. There is no
import path — no flag, no environment variable, no endpoint that can load a key
from anywhere else. Nobody can hand you a key, and you cannot accidentally
deploy someone else's.

**What it does not hold.** No user data. Not one byte of anyone's content
passes through it — no ciphertext, no uploads, no accounts, no cookies, no
logs of who asked for what. It publishes signatures of numbers. There is no
GDPR surface, no retention policy and nothing to report in a breach.

**What uptime you owe.** None. BLS signatures are deterministic, so downtime
means envelopes open later, never that data is lost. The real request is
*"keep 32 bytes safe for years"*, not *"maintain 99.9% uptime"*.

**What you cannot do.** You cannot open an envelope. Your share is one of
several; on its own it reveals nothing. Nor can the BeatTime operator open one
without you.

**Resource cost.** ~16 MB image, ~20 MB RAM, a few kilobytes of traffic a day.

---

## Run it

```bash
docker compose up -d
curl localhost:8080/info
```

That is the whole installation. On first start the container generates its own
key and logs the public half. Send that public key (and your `op` identifier)
to BeatTime — nothing else.

Set exactly one thing before starting, in `compose.yml`:

```yaml
environment:
  BEAT_KEY_OP: "your-operator-id"
```

`BEAT_KEY_OP` identifies **your organisation**, permanently. Lowercase letters,
digits and hyphens, up to 32 characters. It must not name an environment or a
machine — no `-prod`, no `-vps2` — because changing hosting must not change who
you are. The server refuses to start if it does not match.

### Verify the image before running it

The image must be **public and pulled by digest**, never handed to you
privately. That is what keeps your share independent of BeatTime: if the image
were something only you received, whoever built it could have put anything in
it — and would then effectively control your key.

```bash
# beat-key 1.0.0
docker pull ghcr.io/deiflagellum/beat-key@sha256:512b3542aaa3d43f52d0f364d83d681c2b9b2c07d357cd3e55f954f397a30a98

cosign verify ghcr.io/deiflagellum/beat-key@sha256:512b3542aaa3d43f52d0f364d83d681c2b9b2c07d357cd3e55f954f397a30a98   --certificate-identity-regexp 'github.com/DeiFlagellum/beat-key'   --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

The digest for each release is printed in that release's workflow summary.
Images are published for `linux/amd64` and `linux/arm64`.

Builds are reproducible (`CGO_ENABLED=0`, `-trimpath`, `-buildid=`), so you can
rebuild from source and compare rather than take anyone's word. CI enforces
this on every commit — two builds of the same commit must produce a
byte-identical binary, or the build fails:

```bash
git clone https://github.com/DeiFlagellum/beat-key && cd beat-key
git checkout v<version>
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o beat-key .
sha256sum beat-key
```

Compare that against the binary inside the published image. This repository is
public precisely so that this check is possible: an image you cannot rebuild is
an image whose author you have to trust.

---

## The key

It lives at `/data/key.bin` inside the container, on a named Docker volume.

- **A backup of that volume is a copy of the key.** Treat it like a safe, not
  like an application backup.
- **Losing it is permanent.** Every envelope sealed against your public key
  becomes unopenable through your share. If the profile in use has a recovery
  share, holders can still recover; if not, the content is gone.
- **A corrupt key file stops the server.** It deliberately does *not*
  regenerate — a fresh key would silently invalidate every existing envelope.
  Restore from your copy instead.

### Guard against a silently replaced key

The key survives restarts and image upgrades because it lives on the volume,
not in the image. But that protection is only as good as the mount: a typo in
the path, a server rebuilt without that directory, or the wrong compose file
gives an *empty* directory — and the server will then do exactly what it is
designed to do on first start and generate a **new** key. It comes up, answers
requests, and nobody finds out until an envelope fails to open years later.

Set `BEAT_KEY_EXPECT_PUB` to your public key and that becomes a loud refusal
instead:

```yaml
environment:
  BEAT_KEY_OP: your-org
  BEAT_KEY_EXPECT_PUB: "<public_key from /info>"
```

The server then exits with code 3 — **without creating anything** — if the key
is missing or is not the one expected. This is not a key import: the private
key still cannot be injected from outside.

The private key never appears in logs, in any endpoint, or in any output. The
image contains no shell and no package manager, so there is nothing inside the
container to log into.

---

## API

Three endpoints, all `GET`, all public, all CORS-open. Writes return 405.

### `GET /info`

```json
{
  "op": "hetzner-lt",
  "scheme": "bls-unchained-g1-rfc9380",
  "public_key": "s1XtZhQUaNmVoXhs/CGY6qgf…",
  "identity_format": "beattime-beat-v1|<beat_index>",
  "beats_per_day": 1000,
  "beat_index": 20691410,
  "beat_of_day": 410
}
```

### `GET /share/<beat_index>`

Returns the released share — but **only** if that beat has already passed.
A future beat returns `404`, never a partial answer or a hint about how long
is left.

```json
{
  "op": "hetzner-lt",
  "scheme": "bls-unchained-g1-rfc9380",
  "beat_index": 20691310,
  "identity": "3fff2c09a50916558ce67314c98eaf0c…",
  "signature": "hKiQt9IAGxZl+bEwX/0wCvpnlmhyr4Vj…"
}
```

Released shares are immutable and served with a one-year `immutable` cache
header. Mirroring and caching them is encouraged, not tolerated: once a share
is public it is ordinary public data, and the more copies exist, the less the
envelopes depend on this server still being online.

### `GET /healthz`

`200 ok` — for your monitoring, if you want any.

---

## Verify a share yourself

Anyone can check a share against a published public key, offline, without
trusting the server that served it:

```bash
go run ./cmd/verify-share -pub <public_key_b64> -beat <beat_index> -sig <signature_b64>
```

The tool recomputes the identity from the beat index rather than believing the
one in the response — a server that returned a signature over some *other*
identity would produce a share that verifies but does not open anything.

---

## Cryptography

| | |
|---|---|
| Curve | BLS12-381 |
| Signatures | G1 |
| Public keys | G2 |
| Hash-to-curve | RFC 9380, DST `BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_NUL_` |
| Scheme id | `bls-unchained-g1-rfc9380` |
| Identity | `SHA-256("beattime-beat-v1|" + decimal beat_index)` |

The scheme is deliberately identical to drand's unchained G1 chain, so a single
library handles both shares of an envelope and the two sides cannot drift
apart.

Beat arithmetic uses **whole microseconds**, never division by 86.4 — that
value is not representable in binary floating point, and dividing by it drops
the result by one at boundaries:

```
beat_index = unix_microseconds / 86_400_000     (integer division, floored)
```

Index `0` is `1970-01-01T00:00:00Z`. The anchor is UTC, not BMT, so noon UTC is
`@500`. Frozen cross-language vectors live in `internal/beat/beat_test.go`.

---

## Development

```bash
go test ./...        # unit tests, incl. frozen beat vectors
go build .           # server binary
docker build -t beat-key:test .
```

---

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).

The permissive license is part of the point. An operator is asked to rebuild
this image from source and compare it against the published one; a licence that
did not clearly allow that would make the verification legally murky and the
independence claim hollow. Apache-2.0 also carries an express patent grant,
which matters for cryptographic code.

Dependencies keep their own licences (MPL-2.0, MIT, Apache-2.0, BSD-3-Clause)
and are used unmodified — details in [NOTICE](NOTICE).
