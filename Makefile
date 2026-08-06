.PHONY: build-all install full-install update-blocklists test deploy

# Build all binaries
build-all:
	go build -o glocker ./cmd/glocker
	go build -o glocklock ./cmd/glocklock
	go build -o glockpeek ./cmd/glockpeek
	go build -o glockdoc ./cmd/glockdoc

# Rebuild and reinstall
install: build-all
	sudo ./glocker -uninstall "maintenance" -note "upgrade via Make" || true
	sudo ./glocker -install

# Rebuild, update blocklists, and reinstall
full-install: build-all update-blocklists
	sudo ./glocker -uninstall "maintenance" -note "upgrade via Make" || true
	sudo ./glocker -install

# Update blocklists
update-blocklists:
	python3 update_domains.py all

# Run tests
test:
	go test ./...

# Deploy the glockpeek dashboard to your VPS: build a static (CGO-free) linux
# binary, ship it over your existing SSH access, install it, and restart.
#   make deploy DEPLOY_HOST=peek.example.com [DEPLOY_USER=root]
DEPLOY_USER ?= root
DEPLOY_HOST ?=

deploy:
	@test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST, e.g. make deploy DEPLOY_HOST=peek.example.com"; exit 1; }
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/glockpeek ./cmd/glockpeek
	scp dist/glockpeek $(DEPLOY_USER)@$(DEPLOY_HOST):/tmp/glockpeek.new
	ssh $(DEPLOY_USER)@$(DEPLOY_HOST) 'sudo install -m 0755 /tmp/glockpeek.new /usr/local/bin/glockpeek && rm -f /tmp/glockpeek.new && sudo systemctl restart glockpeek && sleep 2 && systemctl is-active glockpeek'
