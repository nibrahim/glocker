output "droplet_ip" {
  value       = digitalocean_droplet.glockpeek.ipv4_address
  description = "Public IPv4 — point your DNS A record here and use it in the Ansible inventory."
}

output "droplet_ipv6" {
  value = digitalocean_droplet.glockpeek.ipv6_address
}

output "dashboard_url" {
  value = var.dns_zone != "" ? "https://${var.dns_record}.${var.dns_zone}" : "set your domain and re-check"
}
