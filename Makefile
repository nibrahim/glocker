.PHONY: build-all install full-install security-check update-blocklists test deploy deploy-landing help

.DEFAULT_GOAL:=help
help: ## display this help message
	@echo "Please use \`make <target>' where <target> is one of"
	@perl -nle'print $& if m{^[a-zA-Z_-]+:.*?## .*$$}' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m  %-25s\033[0m %s\n", $$1, $$2}'




build-all: ## Build all binaries
	go build -o glocker ./cmd/glocker
	go build -o glocklock ./cmd/glocklock
	go build -o glockpeek ./cmd/glockpeek
	go build -o glockdoc ./cmd/glockdoc


install: build-all ## Rebuild and reinstall
	sudo ./glocker -uninstall "maintenance" -note "upgrade via Make" || true
	sudo ./glocker -install


full-install: build-all update-blocklists ## Rebuild, update blocklists, and reinstall
	sudo ./glocker -uninstall "maintenance" -note "upgrade via Make" || true
	sudo ./glocker -install


security-check: build-all ## Report bypass/recovery routes (read-only, needs sudo)
	sudo ./glocker -security-check


update-blocklists: ## Update blocklists
	python3 update_domains.py all


test: ## Run tests
	go test ./...

# Deploy the glockpeek dashboard to your VPS: build a static (CGO-free) linux
# binary, ship it over your existing SSH access, install it, and restart.
#   make deploy DEPLOY_HOST=peek.example.com [DEPLOY_USER=root]
DEPLOY_USER ?= root
DEPLOY_HOST ?= glockerapp.com

deploy: ## Deploy dashboard binary to the VPS
	@test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST, e.g. make deploy DEPLOY_HOST=my.example.com"; exit 1; }
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/glockpeek ./cmd/glockpeek
	scp dist/glockpeek $(DEPLOY_USER)@$(DEPLOY_HOST):/tmp/glockpeek.new
	ssh $(DEPLOY_USER)@$(DEPLOY_HOST) 'sudo install -m 0755 /tmp/glockpeek.new /usr/local/bin/glockpeek && rm -f /tmp/glockpeek.new && sudo systemctl restart glockpeek && sleep 2 && systemctl is-active glockpeek'

# Ship just the static landing page (website/index.html) to the apex web root.
# Same droplet as the dashboard, so DEPLOY_HOST can be the dashboard subdomain.
# For copy tweaks this is all you need — no rebuild, no ansible run.
deploy-landing: ## Deploy the static landing page to the apex web root
	@test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST, e.g. make deploy-landing DEPLOY_HOST=my.example.com"; exit 1; }
	scp website/index.html $(DEPLOY_USER)@$(DEPLOY_HOST):/tmp/glocker-landing.html
	ssh $(DEPLOY_USER)@$(DEPLOY_HOST) 'sudo install -D -o caddy -g caddy -m 0644 /tmp/glocker-landing.html /var/www/glocker-landing/index.html && rm -f /tmp/glocker-landing.html && echo "landing page updated"'
