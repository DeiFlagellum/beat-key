# Protocol

What this server implements, in enough detail to verify the code or write a
second implementation. This is the operator-facing subset of the
`beattime-seal-v1` envelope specification — the parts a key server touches.

---

## 1. Beat index

Time is addressed as an absolute **beat index**. A day is 1000 beats, so one
beat is 86.4 seconds.

```
MICROSECONDS_PER_BEAT = 86_400_000

beat_index = unix_microseconds // MICROSECONDS_PER_BEAT   (floored)
day        = beat_index // 1000
beat_of_day = beat_index % 1000                            ('@NNN')
```

Index `0` is `1970-01-01T00:00:00Z`. The anchor is **UTC**, not BMT — so noon
UTC is `@500`, unlike Swatch Internet Time from 1998, which used UTC+1.

Compute it with whole microseconds. Never divide by `86.4`: that value has no
exact binary floating-point representation, and dividing by it drops the result
by one at beat boundaries. Division must floor, including for negative values —
a language that truncates toward zero (Go, C, Rust) needs an explicit floor, or
it will silently disagree with one that floors (Python) for pre-1970 instants.

Frozen cross-language test vectors are in `internal/beat/beat_test.go`.

---

## 2. Identity

The IBE identity for a beat:

```
identity = SHA-256("beattime-beat-v1|" + decimal_beat_index)
```

ASCII, no padding, no leading zeros on the index. This string is a **frozen
contract**: changing it invalidates every envelope ever sealed against any
operator's key. A change means a new version prefix, never an edit in place.

---

## 3. Signature scheme

| | |
|---|---|
| Curve | BLS12-381 |
| Signatures | G1 (48 bytes compressed) |
| Public keys | G2 (96 bytes compressed) |
| Hash-to-curve | RFC 9380, DST `BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_NUL_` |
| Scheme id | `bls-unchained-g1-rfc9380` |

Deliberately identical to drand's unchained G1 chain. An envelope combines a
share sealed to drand with a share sealed to an operator key, so one library
handles both and the two sides cannot drift apart.

The released share for beat `n` is the BLS signature over `identity(n)`. Being
a BLS signature it is **deterministic**: the same beat always yields the same
bytes, from any correct implementation. That is what makes shares cacheable and
mirrorable.

---

## 4. Release rule

A share for beat `n` may be served **only when `n <= current_beat_index`**.

Any earlier request must return `404` — not `425 Too Early`, not a partial
response, not a hint about how long remains. The response for a future beat must
carry no information beyond "not found". Anything else becomes a side channel
about the key.

The boundary is closed from below: the *current* beat is already released.

---

## 5. Operator identity

Each operator has a stable identifier `op`:

- `[a-z0-9-]`, up to 32 characters
- assigned **once** and never changed
- identifies the **organisation**, never an environment or a machine — no
  `-prod`, no `-vps2`. Moving hosting must not change who you are
- never reused for a different organisation

If an operator ever adds a second key, the two are told apart by the public key
itself; `op` stays the same.

### The public key is the source of truth, not the address

A share fetched from **any** source — a registry, a mirror, an archive, a USB
stick from a stranger — is verified locally against the public key recorded in
the envelope. Which means:

> Once published, a share is **ordinary public data**. The operator is in
> neither the trust path nor the availability path.

A URL in an envelope is therefore only a *hint*. Addresses rot faster than
envelopes mature; the public key does not.

---

## 6. Requirements on a key server

These are what make an operator's share genuinely independent. An
implementation that skips any of them still works — and quietly stops providing
the guarantee the envelope format is sold on.

| | Requirement |
|---|---|
| **R1** | The key is generated **inside the container on first start**, when none is present. There is no import path — no flag, no environment variable, no endpoint that loads a key from elsewhere. Nobody can hand you a key, and you cannot accidentally deploy someone else's. |
| **R2** | The private key never leaves the data volume. No endpoint, log line or debug mode reveals it. |
| **R3** | `GET /info` exposes the public key, `op` and the scheme id. |
| **R4** | `GET /share/<beat_index>` returns a signature **only** for a beat that has passed. Otherwise `404`. |
| **R5** | Zero user data. No ciphertext passes through the server. It publishes signatures of numbers — no retention, no personal data, nothing to report in a breach. |
| **R6** | No uptime obligation. Signatures are deterministic, so downtime means envelopes open later, never that data is lost. The real requirement is *"keep 32 bytes safe for years"*. |
| **R7** | The image is public, built from public source, and pulled by digest — never handed to an operator privately. Otherwise whoever built it effectively controls the operator's key, and the operator's share is not independent at all. |

A corrupt key file must **stop the server**, not trigger regeneration. A fresh
key would silently invalidate every envelope sealed against the old one.

---

## 7. What this does not do

This server is one share holder. On its own it opens nothing, and it cannot
tell you anything about the contents of any envelope — it never sees one. The
envelope's key is split so that a threshold of independent shares is required;
losing or leaking any single share, including this one, reveals nothing.

Two shares held by the same organisation — or by an organisation and someone
who can compel it — count as one. That includes a server running on
infrastructure the envelope's author can administer, regardless of whose name
is on the invoice.
