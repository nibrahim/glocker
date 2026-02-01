.PHONY: build-all install full-install update-blocklists test

# Build all binaries
build-all:
	go build -o glocker ./cmd/glocker
	go build -o glocklock ./cmd/glocklock
	go build -o glockpeek ./cmd/glockpeek

# Rebuild and reinstall
install: build-all
	sudo ./glocker -uninstall "reinstall" || true
	sudo ./glocker -install

# Rebuild, update blocklists, and reinstall
full-install: build-all update-blocklists
	sudo ./glocker -uninstall "reinstall" || true
	sudo ./glocker -install

# Update blocklists
update-blocklists:
	python3 update_domains.py all

# Run tests
test:
	go test ./...
