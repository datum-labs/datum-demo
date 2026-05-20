variable "hcloud_token" {
  description = "Hetzner Cloud API token. Prefer HCLOUD_TOKEN env var over setting this in a .tfvars file."
  type        = string
  sensitive   = true
  default     = null
}

variable "name_prefix" {
  description = "Prefix used for all resource names and labels."
  type        = string
  default     = "demo-datum"
}

# ------------------------------------------------------------------------
# Prober box
# ------------------------------------------------------------------------
variable "location" {
  description = "Hetzner Cloud location for the PROBER. nbg1, fsn1, hel1 (EU); ash, hil (US); sin (Asia)."
  type        = string
  default     = "nbg1"
}

variable "network_zone" {
  description = "Hetzner Cloud network zone for the prober's hcloud_network. Must match --location's zone."
  type        = string
  default     = "eu-central"
}

variable "server_type" {
  description = "Hetzner server type for the prober. cpx21 (~€7.55/mo, 3 vCPU AMD, 4 GB) is the cheapest AMD type still orderable everywhere — cpx11 was retired. The Intel cx-series is EU-only. cax11 (ARM) is cheaper but requires arm64 binaries."
  type        = string
  default     = "cpx21"
}

# ------------------------------------------------------------------------
# Origin box
# ------------------------------------------------------------------------
variable "origin_location" {
  description = "Hetzner Cloud location for the ORIGIN. Pick a different region from --location so the prober's 'direct' probe traverses the public internet. Default ash gives a transatlantic comparison from nbg1."
  type        = string
  default     = "ash"
}

variable "origin_server_type" {
  description = "Hetzner server type for the origin. cpx21 is the cheapest AMD type orderable in every region (cpx11 retired). Note: cx-series is EU-only, so picking ash/hil for origin_location forces cpx or cax."
  type        = string
  default     = "cpx21"
}

variable "origin_payload_bytes" {
  description = "Approximate response body size returned by the origin on every request. Larger payloads make throughput differences more visible; smaller ones make latency the dominant signal."
  type        = number
  default     = 4096
}

variable "origin_hostname" {
  description = "Public hostname for the origin (e.g. origin.demo.datum.shippingbytes.com). If set, Caddy fronts the origin on :443 with Let's Encrypt and the prober's 'direct' target becomes https://<hostname>/ so it can negotiate HTTP/2. Leave blank to keep plain HTTP on :8080. DNS for this hostname must be pointed at origin's IPv4 manually."
  type        = string
  default     = ""
}

# ------------------------------------------------------------------------
# SSH access
# ------------------------------------------------------------------------
variable "ssh_key_name" {
  description = "Name of an SSH key already registered in your Hetzner Cloud project. We look it up by name and attach it to both boxes; we do not upload anything."
  type        = string
}

variable "ssh_source_ips" {
  description = "Source IP ranges allowed to SSH in. Default is wide open — narrow this to your /32 once you know it."
  type        = list(string)
  default     = ["0.0.0.0/0", "::/0"]
}

# ------------------------------------------------------------------------
# Prober configuration
# ------------------------------------------------------------------------
variable "target_direct_url" {
  description = "URL the prober hits over the public internet. Leave blank to auto-compute from origin's public IP."
  type        = string
  default     = null
}

variable "target_datum_url" {
  description = "URL the prober hits via the Datum tunnel. Set this after you've wired up the Datum side."
  type        = string
}

variable "demo_hostname" {
  description = "Public hostname for the UI (e.g. demo.datum.net). If set, Caddy is installed and auto-provisions Let's Encrypt. Leave blank to expose plain HTTP on :8080 of the prober's IP."
  type        = string
  default     = ""
}

# ------------------------------------------------------------------------
# Datum control plane (basic-auth toggle)
# ------------------------------------------------------------------------
variable "datum_tunnel_name" {
  description = "Name of the Datum tunnel HTTPRoute the SecurityPolicy should attach to (e.g. tunnel-vdnmd). When empty, the prober runs without --datum-kubeconfig and the With-Datum 'Basic auth' action is hidden in the UI. When set, the prober ALSO requires two runtime artefacts dropped on the box: /var/lib/demo/kubeconfig.yaml plus a logged-in datumctl session at /var/lib/demo/.datumctl/ — see final_message."
  type        = string
  default     = ""
}

variable "datum_tunnel_namespace" {
  description = "Namespace of the Datum tunnel HTTPRoute. Datum currently only supports 'default'."
  type        = string
  default     = "default"
}
