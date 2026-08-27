# Running a key server — step by step

For the person who will actually type the commands. Ten minutes, start to finish.

You are being asked to hold **one share** of a split key, so that time-sealed
envelopes cannot be opened early by any single party. Your server publishes a
signature once a moment has passed, and nothing else. It never sees anyone's
data.

---

## What your VPS needs

The container is not the constraint. Being able to run Docker at all is — so
the cheapest tier at any provider is enough.

| | Minimum | Why |
|---|---|---|
| CPU | 1 vCPU, any | Signing takes microseconds |
| RAM | **256 MB total** | The container itself uses 2-3 MB, idle and under load alike. The rest is Docker's own footprint |
| Disk | **2 GB free** | Image is 3.4 MB to download, 16 MB unpacked. The key file is 32 bytes |
| Network | A public IPv4 or IPv6, plus a hostname you control | Needed for HTTPS |
| Traffic | A few KB per day | Responses are tiny and cacheable |
| OS | Any Linux with Docker — Debian, Ubuntu, Alma, Rocky | |
| CPU arch | `amd64` or `arm64` | Both are published; a Raspberry Pi works |

These are measured, not estimated. `docker stats` reports 2.1 MiB idle and
2.8 MiB after a burst of requests.

**No uptime guarantee is expected.** Signatures are deterministic, so if your
server is down for a week, envelopes open a week late — nothing is lost. The
real request is *keep 32 bytes safe for years*, not *maintain 99.9%*.

---

## Step 0 — Lock the machine down first

Do this **before** anything else. The key is generated on first start of the
container, and it should be generated on a machine that is already secure — not
on one whose root password is still sitting in a support ticket.

On a freshly provisioned VPS:

```bash
# 1. Change the root password, especially if it was ever sent to anyone
passwd

# 2. Put your SSH key on the box, then turn off password login
ssh-copy-id root@<ip>          # from your own machine
sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
systemctl restart sshd         # keep your current session open until you have tested a new one

# 3. Confirm the clock is right — a wrong clock releases shares at the wrong time
timedatectl
```

Your hosting provider always has hypervisor-level access to the disk; that is
true of every VPS and cannot be avoided. It does not break the scheme, because
opening an envelope needs a threshold of shares and your provider would hold at
most one. It does make one rule concrete, though:

> **Never place two shares with the same hosting provider.** Two operators on
> the same provider look independent and are not.

Tell BeatTime which provider and country you are on, so this can be checked
against the other operators.

### On AlmaLinux, Rocky or Fedora, two extras

These ship with SELinux enforcing and firewalld enabled, which Debian and
Ubuntu do not. Both are good defaults — they just need one command each.

```bash
# Check what you are dealing with
getenforce                     # Enforcing / Permissive / Disabled
systemctl is-active firewalld

# Let the reverse proxy answer on 80 and 443 (step 7)
firewall-cmd --permanent --add-service=http --add-service=https
firewall-cmd --reload
```

For SELinux you do not need to disable anything: the `:Z` on the volume in
step 4 tells Docker to label the directory correctly. If you ever see
`permission denied` on a directory that already has the right owner, SELinux is
the usual reason and `:Z` is the fix.

Do **not** leave port 8080 open to the world. The container binds to
`127.0.0.1` in step 4 for exactly this reason; only the reverse proxy needs to
reach it.

## Step 1 — Agree your operator id

Pick a short name for **your organisation** and tell BeatTime. Lowercase
letters, digits and hyphens, up to 32 characters:

```
uni-krakow      good
acme-hosting    good
acme-prod       rejected — that names an environment, not an organisation
```

It is assigned once and never changes. Moving to different hardware later must
not change who you are.

## Step 2 — Install Docker

Skip if you already have it.

```bash
curl -fsSL https://get.docker.com | sh
```

## Step 3 — Create the data directory

The container runs as an unprivileged user (uid 65532), so the directory has to
belong to it. **Skipping the `chown` is the most common failure** — the
container starts, cannot write, and exits with `permission denied`.

```bash
mkdir -p /srv/beat-key
chown 65532:65532 /srv/beat-key
```

## Step 4 — Write the compose file

`/srv/beat-key/compose.yml`:

```yaml
services:
  beat-key:
    # Pinned by digest, not by tag: a tag can be repointed at a different
    # image, a digest cannot. Ask BeatTime for the current one.
    image: ghcr.io/deiflagellum/beat-key@sha256:9913de07399a36a3a007f7ac95a5597a9b4d72ccea96fdde01e24ccce1b2df03
    container_name: beat-key
    restart: unless-stopped
    environment:
      BEAT_KEY_OP: "your-org-id"        # from step 1
    volumes:
      # `:Z` labels the directory for SELinux (AlmaLinux, Rocky, Fedora).
      # It is ignored on systems without SELinux, so it is safe everywhere.
      - /srv/beat-key:/data:Z           # the only place the key exists
    ports:
      - "127.0.0.1:8080:8080"           # localhost only; HTTPS comes in step 7
    read_only: true
    cap_drop: ["ALL"]
    security_opt: ["no-new-privileges:true"]
    logging:
      driver: json-file
      options: { max-size: "10m", max-file: "3" }
```

## Step 5 — Start it and note your public key

```bash
cd /srv/beat-key
docker compose up -d
docker logs beat-key | head -2
```

You will see, once and only once:

```
wygenerowano NOWY klucz operatora ...
beat-key gotowy op=your-org-id ... public_key=<long base64 string>
```

**Copy that `public_key`.** You will need it in the next step and BeatTime
needs it too. The private half never leaves `/srv/beat-key` — nobody, including
BeatTime, can ask you for it, and there is no command that prints it.

## Step 6 — Turn on the safety catch

The key survives restarts and image upgrades because it lives on disk, not in
the image. But that protection is only as good as the mount: a typo in the
path, or the server rebuilt without that directory, gives an *empty* directory —
and the container will then do what it is designed to do on first start and
generate a **new** key. It comes up, answers requests, and nobody finds out
until an envelope fails to open years later.

Add your public key to the compose file, under `environment:`:

```yaml
      BEAT_KEY_EXPECT_PUB: "<the public_key from step 5>"
```

```bash
docker compose up -d
docker logs beat-key | tail -2
```

The log must now say `klucz zgodny z BEAT_KEY_EXPECT_PUB`. From here on, a
wrong or missing key stops the container with exit code 3 instead of silently
starting a new life.

## Step 7 — Put it behind HTTPS

The service must be reachable from the internet over HTTPS. With Caddy this is
three lines — it obtains and renews the certificate itself:

`/etc/caddy/Caddyfile`

```
key.your-domain.example {
    reverse_proxy 127.0.0.1:8080
}
```

```bash
systemctl reload caddy
```

nginx or Traefik work equally well; nothing about the service is special.

## Step 8 — Check it from outside

From any other machine:

```bash
curl -s https://key.your-domain.example/info
```

You should get your `op`, your public key and the current beat index. Then:

```bash
BEAT=$(curl -s https://key.your-domain.example/info | python3 -c 'import sys,json;print(json.load(sys.stdin)["beat_index"])')

curl -s -o /dev/null -w 'past:   %{http_code}  (expect 200)\n' https://key.your-domain.example/share/$((BEAT-3))
curl -s -o /dev/null -w 'future: %{http_code}  (expect 404)\n' https://key.your-domain.example/share/$((BEAT+1000))
```

The second one matters most. A server that answers for a *future* beat would
release shares early and break every envelope it takes part in. BeatTime checks
this before adding you to the registry.

## Step 9 — Back up the key

```bash
tar czf /root/beat-key-backup-$(date +%F).tar.gz -C /srv/beat-key .
```

Move that file somewhere off this machine. **It is a copy of the key** — treat
it like a safe, not like an application backup. On this machine alone it
protects you against a mistake, not against losing the machine.

## Step 10 — Send BeatTime three things

```
op:         your-org-id
public key: <from step 5>
url:        https://key.your-domain.example
```

That is everything. There is nothing to send back to you, and no credential to
exchange.

---

## What you are and are not signing up for

**You cannot open an envelope.** Your share is one of several; on its own it
reveals nothing. Neither can BeatTime without you — that is the entire point of
asking you.

**You hold no user data.** Not one byte of anyone's content passes through the
server: no ciphertext, no uploads, no accounts, no cookies, no log of who asked
for what. It publishes signatures of numbers. There is no GDPR surface, no
retention policy, and nothing to report in a breach.

**You owe no availability.** See the note at the top: downtime delays, it does
not destroy.

**The one real obligation** is keeping `/srv/beat-key` alive. If it is lost,
every envelope that depends on your share loses that share.

## Three things never to do

1. **Never delete or recreate the data directory** once envelopes exist against
   your key.
2. **Never accept a key from anyone**, including BeatTime. There is deliberately
   no way to import one. If someone offers you a key file, something is wrong.
3. **Never run an image someone sent you privately.** Pull it from the public
   registry by digest. The source is public at
   <https://github.com/DeiFlagellum/beat-key> precisely so that you can rebuild
   it and compare — an image you cannot rebuild is an image whose author you
   have to trust, and then your share is not independent at all.

## Verifying the image yourself

Optional, and the reason the repository is public:

```bash
git clone https://github.com/DeiFlagellum/beat-key && cd beat-key
git checkout v1.1.0
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o beat-key .
sha256sum beat-key
```

Builds are reproducible, and CI fails if two builds of the same commit ever
differ. You can also check the signature on the published image:

```bash
cosign verify ghcr.io/deiflagellum/beat-key@sha256:9913de07399a36a3a007f7ac95a5597a9b4d72ccea96fdde01e24ccce1b2df03 \
  --certificate-identity-regexp 'github.com/DeiFlagellum/beat-key' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## If something goes wrong

| Symptom | Cause |
|---|---|
| `permission denied` on start | Step 3 skipped — `chown 65532:65532` on the data directory. If the owner is already right, it is SELinux: make sure the volume line ends in `:Z` |
| Nothing answers on 443 | firewalld — see the AlmaLinux/Rocky note above |
| Exit code 3 | Safety catch fired: the data directory is empty or holds a different key. Do **not** delete anything; check the mount path first |
| `404` for a past beat | The server's clock is wrong. Check NTP |
| Refuses to start, complains about `BEAT_KEY_OP` | The id names an environment (`-prod`, `-vps`) or uses characters outside `[a-z0-9-]` |

Full protocol, if you want to know exactly what the server does:
[`PROTOCOL.md`](PROTOCOL.md).
