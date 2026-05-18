terraform {
  required_version = ">= 1.6"
  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = "~> 1.50"
    }
  }
}

provider "hcloud" {
  # Set HCLOUD_TOKEN in the environment, or pass via -var="hcloud_token=..."
  token = var.hcloud_token
}

# ------------------------------------------------------------------------
# Network (Hetzner's VPC equivalent). The prober lives here; the origin
# does NOT — it deliberately sits in a different region so the prober's
# "direct" probe traverses the real public internet.
# ------------------------------------------------------------------------
resource "hcloud_network" "demo" {
  name     = "${var.name_prefix}-net"
  ip_range = "10.0.0.0/16"
  labels   = local.labels
}

resource "hcloud_network_subnet" "demo" {
  network_id   = hcloud_network.demo.id
  type         = "cloud"
  network_zone = var.network_zone
  ip_range     = "10.0.1.0/24"
}

# ------------------------------------------------------------------------
# Firewalls. Two of them so the security posture is explicit.
#
# - Prober: 22 from your IP, 80 + 443 from anywhere (Caddy or plain).
# - Origin: 22 from your IP, 8080 from anywhere (the prober hits this
#           directly over the public internet for the "without Datum"
#           comparison).
# ------------------------------------------------------------------------
resource "hcloud_firewall" "prober" {
  name   = "${var.name_prefix}-prober-fw"
  labels = local.labels

  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "22"
    source_ips = var.ssh_source_ips
  }
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "80"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "443"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
  rule {
    direction  = "in"
    protocol   = "icmp"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
}

resource "hcloud_firewall" "origin" {
  name   = "${var.name_prefix}-origin-fw"
  labels = local.labels

  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "22"
    source_ips = var.ssh_source_ips
  }
  rule {
    direction  = "in"
    protocol   = "tcp"
    port       = "8080"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
  rule {
    direction  = "in"
    protocol   = "icmp"
    source_ips = ["0.0.0.0/0", "::/0"]
  }
}

# ------------------------------------------------------------------------
# SSH key — looked up by name from your Hetzner project. Add it once via
# the Hetzner console (or hcloud CLI) and reference it here; Terraform
# does not manage the key's lifecycle.
# ------------------------------------------------------------------------
data "hcloud_ssh_key" "default" {
  name = var.ssh_key_name
}

# ------------------------------------------------------------------------
# Origin server. Deliberately in a different region from the prober so the
# "without Datum" probe traverses the public internet rather than
# Hetzner's intra-DC fabric.
# ------------------------------------------------------------------------
resource "hcloud_server" "origin" {
  name         = "${var.name_prefix}-origin"
  server_type  = var.origin_server_type
  image        = "debian-12"
  location     = var.origin_location
  ssh_keys     = [data.hcloud_ssh_key.default.id]
  firewall_ids = [hcloud_firewall.origin.id]

  labels = merge(local.labels, { role = "origin" })

  user_data = templatefile("${path.module}/cloud-init-origin.yaml", {
    payload_bytes = var.origin_payload_bytes
  })
}

# ------------------------------------------------------------------------
# Prober + UI server. Hits origin.ipv4_address for the "direct" path; the
# "datum" URL is whatever you wired up manually in your Datum project.
# ------------------------------------------------------------------------
resource "hcloud_server" "prober" {
  name         = "${var.name_prefix}-prober"
  server_type  = var.server_type
  image        = "debian-12"
  location     = var.location
  ssh_keys     = [data.hcloud_ssh_key.default.id]
  firewall_ids = [hcloud_firewall.prober.id]

  labels = merge(local.labels, { role = "prober" })

  user_data = templatefile("${path.module}/cloud-init-prober.yaml", {
    target_direct = local.target_direct_url
    target_datum  = var.target_datum_url
    hostname      = var.demo_hostname
    enable_caddy  = var.demo_hostname != ""
  })

  network {
    network_id = hcloud_network.demo.id
  }

  depends_on = [hcloud_network_subnet.demo]
}

locals {
  labels = {
    project = "demo-datum-net"
    managed = "terraform"
  }

  # Auto-compute the "direct" URL from the origin's public IP unless the
  # caller overrode it (handy for testing against a third-party origin).
  target_direct_url = (
    var.target_direct_url != null && var.target_direct_url != ""
    ? var.target_direct_url
    : "http://${hcloud_server.origin.ipv4_address}:8080/"
  )
}
