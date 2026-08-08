# Provision + configure the hosted glockpeek VPS.
#
# One-time setup:
#   cp terraform/terraform.tfvars.example terraform/terraform.tfvars
#   # put your DigitalOcean token + ssh key name + region in it (gitignored)
#   cp ansible/group_vars/all.example.yml ansible/group_vars/all.yml
#   # set the domain + passwords, then: ansible-vault encrypt ansible/group_vars/all.yml
#
# Then:
#   make init       # download the DO provider (one-time)
#   make plan       # review what will be created — no changes
#   make server     # create the droplet (BILLABLE)
#   make configure  # provision it: postgres, glockpeek, caddy, account + token
#   make up         # server + configure
#   make ip         # print the droplet's IP
#   make destroy    # tear everything down

.PHONY: init fmt validate plan server ip configure up destroy

TF := terraform -chdir=terraform

init:
	$(TF) init

fmt:
	$(TF) fmt -recursive

validate: init
	$(TF) validate

plan: init
	$(TF) plan

# `terraform apply` asks for confirmation before it creates anything.
server: init
	$(TF) apply

ip:
	@$(TF) output -raw droplet_ip

# Provision the droplet. Builds glockpeek locally and ships it — no push needed.
# Set VAULT=--ask-vault-pass if you encrypted group_vars/all.yml.
# Set INV=1 to regenerate the inventory from terraform state (needs `make server`
# to have run here); otherwise it uses the existing ansible/inventory.ini.
VAULT ?=
configure:
	@command -v ansible-playbook >/dev/null || { echo "install ansible first"; exit 1; }
	@if [ "$(INV)" = "1" ]; then printf '[glockpeek]\n%s ansible_user=root\n\n[glockpeek:vars]\nansible_python_interpreter=/usr/bin/python3\n' "$$($(TF) output -raw droplet_ip)" > ansible/inventory.ini; fi
	ansible-playbook -i ansible/inventory.ini ansible/playbook.yml $(VAULT)

up: server configure

# `terraform destroy` also asks for confirmation.
destroy: init
	$(TF) destroy
