variable "do_token" {
  type        = string
  sensitive   = true
  description = "DigitalOcean API token (dop_v1_...)."
}

variable "project_name" {
  type        = string
  default     = "Personal"
  description = "Existing DO project to place the droplet under."
}

variable "ssh_key_name" {
  type        = string
  description = "Name of an SSH key already uploaded to your DO account."
}

variable "region" {
  type        = string
  default     = "blr1"
  description = "DO region slug (blr1 = Bangalore)."
}

variable "droplet_size" {
  type        = string
  default     = "s-1vcpu-1gb"
  description = "Droplet size slug. 1GB is plenty for a single-user dashboard."
}

variable "droplet_image" {
  type    = string
  default = "ubuntu-24-04-x64"
}

variable "droplet_name" {
  type    = string
  default = "glockpeek"
}

variable "ssh_source_addresses" {
  type        = list(string)
  default     = ["0.0.0.0/0", "::/0"]
  description = "Who may reach SSH (22). Tighten to your own IP/CIDR."
}

# Optional DNS — only if the zone is managed on DigitalOcean.
variable "manage_dns" {
  type    = bool
  default = false
}

variable "dns_zone" {
  type        = string
  default     = ""
  description = "Apex domain managed on DO, e.g. example.com."
}

variable "dns_record" {
  type        = string
  default     = "peek"
  description = "Subdomain host for the dashboard (peek -> peek.example.com)."
}
