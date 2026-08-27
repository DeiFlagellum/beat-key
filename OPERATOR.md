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

**On the server:**

```bash
# Change the root password, especially if it was ever sent to anyone
passwd

# Confirm the clock is right — a wrong clock releases shares at the wrong time
timedatectl

# Find out what you are dealing with (see the note below)
getenforce
systemctl is-active firewalld
```

**SSH keys are recommended, not required.** If you would rather keep password
login, skip to *Staying with a password* below.

**On your own computer — not on the server:**

```bash
ssh-keygen -t ed25519          # skip if you already have a key
ssh-copy-id root@<ip>
```

On Windows there is no `ssh-copy-id`; use PowerShell instead:

```powershell
type $env:USERPROFILE\.ssh\id_ed25519.pub | ssh root@<ip> `
  "mkdir -p ~/.ssh && chmod 700 ~/.ssh && cat >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
```

**Only after key login works**, disable password authentication. Open a
*second* terminal, confirm `ssh root@<ip>` lets you in without asking for a
password, and keep your first session open the whole time — that session is
your way back if anything goes wrong:

```bash
sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
systemctl restart sshd
```

### Staying with a password

Perfectly workable — the machine holds one share, not the envelopes. Two things
make it a fair trade, because an open SSH port on a public IP is scanned within
minutes of existing:

```bash
# A long, unique password — not one used anywhere else, and not the one from
# the provider's welcome email or support ticket
passwd

# Rate-limit the guessing
dnf install -y fail2ban && systemctl enable --now fail2ban
```

What matters far more than which method you pick is that the password you were
originally given — the one that travelled by e-mail or through a support
ticket — is no longer in use.

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

A **minimal** AlmaLinux/Rocky install may have no firewall at all — not even
`firewall-cmd`. On a machine meant to run unattended for years, three things are
worth the five minutes, roughly in this order of value:

```bash
# 1. Security updates that apply themselves. Highest value of the three:
#    nobody will be watching this box in two years.
dnf install -y dnf-automatic
sed -i 's/^upgrade_type =.*/upgrade_type = security/;s/^apply_updates =.*/apply_updates = yes/'     /etc/dnf/automatic.conf
systemctl enable --now dnf-automatic.timer

# 2. Rate-limit SSH guessing (essential if you kept password login)
dnf install -y epel-release && dnf install -y fail2ban fail2ban-firewalld
printf '[sshd]
enabled = true
maxretry = 5
bantime = 1h
' > /etc/fail2ban/jail.local
systemctl enable --now fail2ban

# 3. Let in only what is needed. Add ssh BEFORE reloading, or you lock
#    yourself out of your own session.
dnf install -y firewalld && systemctl enable --now firewalld
firewall-cmd --permanent --add-service=ssh
firewall-cmd --permanent --add-service=http --add-service=https
firewall-cmd --reload
```

If firewalld was already `active`, only the `firewall-cmd` lines apply.

> **Docker writes its own nftables rules, and published ports bypass the
> firewall.** If the ports line in step 4 were ever "simplified" to
> `"8080:8080"`, port 8080 would be reachable from the internet despite
> firewalld — and `firewall-cmd --list-all` would not show it. What actually
> keeps it private is the `127.0.0.1:` prefix. Leave it there.

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

**Debian / Ubuntu:**

```bash
curl -fsSL https://get.docker.com | sh
```

**AlmaLinux / Rocky:** the convenience script above refuses to run — it matches
on the `ID` in `/etc/os-release` and its list has `centos` and `rhel` but not
`almalinux`. That is a gap in the script, not in the packages: Docker publishes
EL builds under the `centos` path and they install unchanged here.

```bash
curl -fsSL https://download.docker.com/linux/centos/docker-ce.repo   -o /etc/yum.repos.d/docker-ce.repo

dnf install -y docker-ce docker-ce-cli containerd.io   docker-buildx-plugin docker-compose-plugin

systemctl enable --now docker
docker --version && docker compose version
```

If `dnf` reports a 404 on the metadata, `$releasever` expanded to something
other than the major version. One fix:

```bash
sed -i 's|/centos/$releasever/|/centos/10/|' /etc/yum.repos.d/docker-ce.repo
dnf clean all
```

Adding the repository is better than the script anyway: Docker updates then
arrive through `dnf` like everything else, including under `dnf-automatic`.

**Then check that the kernel can actually do container networking:**

```bash
find /lib/modules/$(uname -r) -name 'xt_addrtype*' | grep -q .   && echo "OK"   || echo "MISSING — see below"
```

Minimal EL images install `kernel-core` and `kernel-modules-core` but often not
`kernel-modules-extra`, which is where `xt_addrtype` lives. Docker needs it to
set up its NAT rules, and installing Docker does not pull kernel modules as a
dependency — so the daemon installs cleanly and then refuses to start with:

```
'iptables -t nat -A PREROUTING -m addrtype --dst-type LOCAL -j DOCKER' failed:
Warning: Extension addrtype revision 0 not supported, missing kernel module?
```

The fix, and note the second half — a freshly reinstalled VPS is often still
booted into an older kernel than the newest one installed, and modules only
exist for the kernel they were built for:

```bash
dnf install -y kernel-modules-extra
uname -r                                   # running kernel
rpm -qa 'kernel-modules-extra*'            # which kernels have the modules

# If those two do not match, reboot — you also want the newer kernel anyway
reboot
```

After the reboot, `systemctl status docker` should be green. Confirm the whole
path — daemon, networking, image pull — before going further:

```bash
docker run --rm hello-world
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
