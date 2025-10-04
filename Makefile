.PHONY: build install uninstall clean status dry-run test

BINARY_NAME=glocker
INSTALL_PATH=/usr/local/bin/$(BINARY_NAME)
SERVICE_FILE=extras/$(BINARY_NAME).service
SERVICE_PATH=/etc/systemd/system/$(SERVICE_FILE)

build:
	@go build -o $(BINARY_NAME) -ldflags="-s -w" glocker.go

install: build
	@sudo cp $(BINARY_NAME) $(INSTALL_PATH)
	@sudo chmod +x $(INSTALL_PATH)
	@sudo $(INSTALL_PATH) -install
	@sudo cp $(SERVICE_FILE) $(SERVICE_PATH)
	@sudo systemctl daemon-reload
	@sudo systemctl enable $(SERVICE_FILE)
	@sudo systemctl start $(SERVICE_FILE)

uninstall:
	@sudo systemctl stop $(SERVICE_FILE) 2>/dev/null || true
	@sudo systemctl disable $(SERVICE_FILE) 2>/dev/null || true
	@sudo rm -f $(SERVICE_PATH)
	@sudo systemctl daemon-reload
	@sudo chattr -i $(INSTALL_PATH) 2>/dev/null || true
	@sudo rm -f $(INSTALL_PATH)
	@sudo chattr -i /etc/hosts 2>/dev/null || true
	@sudo iptables -S OUTPUT | grep 'DISTRACTION-BLOCK' | sed 's/-A/-D/' | xargs -r -L1 sudo iptables
	@sudo ip6tables -S OUTPUT | grep 'DISTRACTION-BLOCK' | sed 's/-A/-D/' | xargs -r -L1 sudo ip6tables

status:
	@$(BINARY_NAME)

dry-run: build
	@sudo ./$(BINARY_NAME) -dry-run

test: build
	@sudo ./$(BINARY_NAME) -once

clean:
	@rm -f $(BINARY_NAME)
