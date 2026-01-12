.PHONY: build-all reinstall update-blocklists test

# Build both glocker and glocklock binaries
build-all:
	go build -o glocker ./cmd/glocker
	go build -o glocklock ./cmd/glocklock

# Rebuild and reinstall
reinstall: build-all
	sudo ./glocker -uninstall "reinstall" || true
	sudo ./glocker -install

# Update blocklists
update-blocklists:
	python3 update_domains.py all

# Run tests
test:
	go test ./...
