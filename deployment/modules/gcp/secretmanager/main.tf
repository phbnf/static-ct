terraform {
  required_providers {
    google = {
      source  = "registry.terraform.io/hashicorp/google"
      version = "6.12.0"
    }
  }
}

# Secret Manager

resource "google_project_service" "secretmanager_googleapis_com" {
  service            = "secretmanager.googleapis.com"
  disable_on_destroy = false
}

# ECDSA key with P256 elliptic curve. Do NOT use this in production environment.
#
# Security Notice
# The private key generated by this resource will be stored unencrypted in your 
# Terraform state file. Use of this resource for production deployments is not 
# recommended.
#
# See https://registry.terraform.io/providers/hashicorp/tls/latest/docs/resources/private_key.
resource "tls_private_key" "tesseract_ecdsa_p256" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "google_secret_manager_secret" "tesseract_ecdsa_p256_public_key" {
  secret_id = "${var.base_name}-ecdsa-p256-public-key"

  labels = {
    label = "tesseract-public-key"
  }

  replication {
    auto {}
  }

  depends_on = [google_project_service.secretmanager_googleapis_com]
}

resource "google_secret_manager_secret_version" "tesseract_ecdsa_p256_public_key" {
  secret = google_secret_manager_secret.tesseract_ecdsa_p256_public_key.id

  secret_data = tls_private_key.tesseract_ecdsa_p256.public_key_pem
}

resource "google_secret_manager_secret" "tesseract_ecdsa_p256_private_key" {
  secret_id = "${var.base_name}-ecdsa-p256-private-key"

  labels = {
    label = "tesseract-private-key"
  }

  replication {
    auto {}
  }

  depends_on = [google_project_service.secretmanager_googleapis_com]
}

resource "google_secret_manager_secret_version" "tesseract_ecdsa_p256_private_key" {
  secret = google_secret_manager_secret.tesseract_ecdsa_p256_private_key.id

  secret_data = tls_private_key.tesseract_ecdsa_p256.private_key_pem
}
