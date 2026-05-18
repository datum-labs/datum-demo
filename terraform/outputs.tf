output "prober_ipv4" {
  description = "Public IPv4 of the prober box."
  value       = hcloud_server.prober.ipv4_address
}

output "prober_ipv6" {
  description = "Public IPv6 of the prober box."
  value       = hcloud_server.prober.ipv6_address
}

output "origin_ipv4" {
  description = "Public IPv4 of the origin box."
  value       = hcloud_server.origin.ipv4_address
}

output "origin_ipv6" {
  description = "Public IPv6 of the origin box."
  value       = hcloud_server.origin.ipv6_address
}

output "ssh_prober" {
  description = "SSH one-liner for the prober box."
  value       = "ssh root@${hcloud_server.prober.ipv4_address}"
}

output "ssh_origin" {
  description = "SSH one-liner for the origin box."
  value       = "ssh root@${hcloud_server.origin.ipv4_address}"
}

output "deploy_commands" {
  description = "Build the binaries and ship them to both boxes."
  value = {
    all    = "../deploy.sh"
    origin = "../deploy.sh root@${hcloud_server.origin.ipv4_address} origin"
    prober = "../deploy.sh root@${hcloud_server.prober.ipv4_address} prober"
  }
}

output "target_direct_url" {
  description = "What the prober uses for the 'direct' probe (auto-computed from origin unless overridden)."
  value       = local.target_direct_url
}

output "ui_url" {
  description = "Where to point your browser once both binaries are deployed and the services are up."
  value = var.demo_hostname != "" ? (
    "https://${var.demo_hostname}/  (after you point DNS at ${hcloud_server.prober.ipv4_address})"
    ) : (
    "http://${hcloud_server.prober.ipv4_address}:8080/"
  )
}
