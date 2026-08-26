# Deploying a hosted glockpeek on DigitalOcean

This provisions a small DigitalOcean droplet (under your **Personal** project) and
configures it to run **glockpeek** in hosted mode — Postgres-backed, behind Caddy
with automatic HTTPS. Your laptop keeps running the `glocker` agent as usual; it
just syncs to this box over the internet instead of to a local glockpeek.

```
deploy/
  terraform/   # provisions the droplet + firewall (+ optional DNS)
  ansible/     # configures the droplet: postgres, glockpeek, caddy, account, token
```

## What you need locally

- `terraform` (>= 1.5) and `ansible` (>= 2.15)
- A DigitalOcean API token, and an SSH key already uploaded to your DO account
- Two hostnames pointing at the droplet (DNS on DO or anywhere): an **apex** for
  the static landing page (e.g. `glockerapp.com`) and a **subdomain** for the
  dashboard app (e.g. `my.glockerapp.com`) — one A record each
- `ansible-galaxy collection install -r ansible/requirements.yml`

## 1. Provision the droplet

```bash
cd terraform
cp terraform.tfvars.example terraform.tfvars   # fill in token, ssh key name, region, domain
terraform init
terraform apply
terraform output droplet_ip
```

This creates the droplet, attaches it to your **Personal** project, and adds a
firewall (22/80/443). If your domain's DNS is on DigitalOcean, set `manage_dns`
and it creates the A record too; otherwise point an A record at the output IP
yourself and wait for it to propagate.

## 2. Set up the machine (once)

The Ansible playbook is a **one-time, idempotent** provision — Postgres, Caddy,
the service user, config, the systemd unit, the firewall, and the static landing
page (copied from `website/` to the apex domain's web root). It does **not**
build or ship the glockpeek binary; that's `make deploy`.

```bash
cd ../ansible
cp inventory.example.ini inventory.ini            # put the droplet IP in
cp group_vars/all.example.yml group_vars/all.yml  # set landing_domain + dashboard_domain, DB + admin passwords
# optional but recommended: ansible-vault encrypt group_vars/all.yml
```

Then, from `deploy/`:

```bash
make configure                         # plaintext all.yml
make configure VAULT=--ask-vault-pass  # if you vaulted it
```

## 3. Deploy glockpeek

The binary is built locally from your working tree and shipped over — nothing is
built on the box, nothing needs to be on GitHub. From the **repo root**:

```bash
make deploy DEPLOY_HOST=my.example.com
```

`DEPLOY_HOST` is the **dashboard** subdomain. This starts the service; Caddy
fetches TLS certs for both hostnames on first request (both must already resolve
to the droplet). The apex serves the landing page from `website/`; to update it,
edit `website/index.html` and re-run `make configure`.

## 4. Create your account + ingest token (once)

These need the binary, so run them after the first deploy:

```bash
ssh root@<droplet> "echo 'your-password' | sudo -u glockpeek glockpeek -adduser noufal"
ssh root@<droplet> "sudo -u glockpeek glockpeek -addtoken noufal"
```

The second command prints the **ingest token** once — copy it.

## 5. Point your laptop's agent at it

In your local `conf/conf.yaml`:

```yaml
sync:
  enabled: true
  glockpeek_url: "https://my.example.com"   # your domain
  token: "<the ingest token from step 4>"
  interval_seconds: 300
```

Then `sudo glocker -reload` (or `make full-install`). The agent now backfills and
syncs to the hosted dashboard. Log in at `https://my.example.com` with the admin
account you created.

Re-deploys after that are just `make deploy DEPLOY_HOST=...`; re-run the playbook
only when the box's *setup* changes.

## Notes

- glockpeek runs as a dedicated non-root `glockpeek` user — it needs no
  privileges, just its Postgres DSN.
- Caddy handles TLS automatically (Let's Encrypt); `glockpeek_secure_cookies` is
  on, so sessions are HTTPS-only.
- Nothing here touches the `glocker` agent on your laptop — this is only the
  dashboard half.
- **Never commit** `terraform.tfvars`, `*.tfstate`, `inventory.ini`, or an
  un-vaulted `all.yml`. The included `.gitignore` covers them.
