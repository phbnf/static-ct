terraform {
  backend "gcs" {}

  required_providers {
    google = {
      source  = "registry.terraform.io/hashicorp/google"
      version = "6.12.0"
    }
  }
}

# Artifact Registry

module "artifactregistry" {
  source = "../../artifactregistry"

  location   = var.location
  docker_env = var.docker_env
}

# Cloud Build

locals {
  cloudbuild_service_account   = "cloudbuild-${var.env}-sa@${var.project_id}.iam.gserviceaccount.com"
  artifact_repo                = "${var.location}-docker.pkg.dev/${var.project_id}/${module.artifactregistry.docker.name}"
  conformance_gcp_docker_image = "${local.artifact_repo}/conformance-gcp"
}

resource "google_project_service" "cloudbuild_api" {
  service            = "cloudbuild.googleapis.com"
  disable_on_destroy = false
}

## Service usage API is required on the project to enable APIs.
## https://cloud.google.com/apis/docs/getting-started#enabling_apis
## serviceusage.googleapis.com acts as a central point for managing the API 
## lifecycle within your project. By ensuring the required APIs are enabled 
## and accessible, it allows Cloud Build to function seamlessly and interact 
## with other Google Cloud services as needed.
## 
## The Cloud Build service account also needs roles/serviceusage.serviceUsageViewer.
resource "google_project_service" "serviceusage_api" {
  service            = "serviceusage.googleapis.com"
  disable_on_destroy = false
}

resource "google_cloudbuild_trigger" "build_trigger" {
  name            = "build-docker-${var.docker_env}"
  service_account = "projects/${var.project_id}/serviceAccounts/${local.cloudbuild_service_account}"
  location        = var.location

  # TODO(phboneff): use a better mechanism to trigger releases that re-uses Docker containters, or based on branches rather.
  # This is a temporary mechanism to speed up development.
  github {
    owner = var.github_owner
    name  = "static-ct"
    push {
      tag = "^staging-deploy-(.+)$"
    }
  }

  build {
    ## Build TesseraCT GCP Docker image.
    ## This will be used by the building the conformance Docker image which includes 
    ## the test data.
    step {
      id   = "docker_build_sctfe_gcp"
      name = "gcr.io/cloud-builders/docker"
      args = [
        "build",
        "-t", "tesseract-gcp:$SHORT_SHA",
        "-t", "tesseract-gcp:latest",
        "-f", "./cmd/gcp/Dockerfile",
        "."
      ]
    }

    ## Build the SCTFE GCP Conformance Docker container image.
    step {
      id   = "docker_build_conformance_gcp"
      name = "gcr.io/cloud-builders/docker"
      args = [
        "build",
        "-t", "${local.conformance_gcp_docker_image}:$SHORT_SHA",
        "-t", "${local.conformance_gcp_docker_image}:latest",
        "-f", "./cmd/gcp/staging/Dockerfile",
        "."
      ]
      wait_for = ["docker_build_sctfe_gcp"]
    }

    ## Push the conformance Docker container image to Artifact Registry.
    step {
      id   = "docker_push_conformance_gcp"
      name = "gcr.io/cloud-builders/docker"
      args = [
        "push",
        "--all-tags",
        local.conformance_gcp_docker_image
      ]
      wait_for = ["docker_build_conformance_gcp"]
    }

    ## Apply the deployment/live/gcp/static-staging/logs/arche2025h1 terragrunt config.
    ## This will bring up or update the conformance infrastructure, including a service
    ## running the conformance server docker image built above.
    step {
      id     = "terraform_apply_conformance_staging"
      name   = "alpine/terragrunt"
      script = <<EOT
        terragrunt --terragrunt-non-interactive --terragrunt-no-color apply -auto-approve -no-color 2>&1
        terragrunt --terragrunt-no-color output --raw conformance_url -no-color > /workspace/conformance_url
        terragrunt --terragrunt-no-color output --raw conformance_bucket_name -no-color > /workspace/conformance_bucket_name
        terragrunt --terragrunt-no-color output --raw ecdsa_p256_public_key_data -no-color > /workspace/conformance_log_public_key.pem
        cat /workspace/conformance_url
        cat /workspace/conformance_bucket_name
        cat /workspace/conformance_log_public_key.pem
      EOT
      dir    = "deployment/live/gcp/static-ct-staging/logs/arche2025h1"
      env = [
        "GOOGLE_PROJECT=${var.project_id}",
        "TF_IN_AUTOMATION=1",
        "TF_INPUT=false",
        "TF_VAR_project_id=${var.project_id}",
        "DOCKER_CONTAINER_TAG=$SHORT_SHA"
      ]
      wait_for = ["docker_push_conformance_gcp"]
    }

    ## Since the conformance infrastructure is not publicly accessible, we need to use 
    ## bearer tokens for the test to access them.
    ## This step creates those, and stores them for later use.
    step {
      id       = "bearer_token"
      name     = "gcr.io/cloud-builders/gcloud"
      script   = <<EOT
        gcloud auth print-access-token > /workspace/cb_access
        curl -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/${local.cloudbuild_service_account}/identity?audience=$(cat /workspace/conformance_url)" > /workspace/cb_identity
      EOT
      wait_for = ["terraform_apply_conformance_staging"]
    }

    ## TODO(phboneff): move to its own container.
    ## Test against the conformance server with CT Preloader.
    ## Leave enough time for the preloader to run, until the token expires.
    timeout = "3600s" // 60 minutes
    step {
      id       = "ct_preloader"
      name     = "golang"
      script   = <<EOT
	      START_INDEX=$(curl -H "Authorization: Bearer $(cat /workspace/cb_access)" https://storage.googleapis.com/$(cat /workspace/conformance_bucket_name)/checkpoint | head -2 | tail -1)
	      echo "Will start preloader at index $START_INDEX"
        go run github.com/google/certificate-transparency-go/preload/preloader@master \
          --target_log_uri=$(cat /workspace/conformance_url)/arche2025h1.ct.transparency.dev \
	        --target_bearer_token="$(cat /workspace/cb_identity)" \
          --source_log_uri=https://ct.googleapis.com/logs/us1/argon2025h1 \
	        --start_index=$START_INDEX \
          --num_workers=10 \
          --parallel_fetch=10 \
          --parallel_submit=10
      EOT
      wait_for = ["bearer_token"]
      timeout = "3000s" // 50 minutes
    }

    options {
      logging      = "CLOUD_LOGGING_ONLY"
      machine_type = "E2_HIGHCPU_8"
    }
  }

  depends_on = [
    module.artifactregistry
  ]
}

resource "google_cloud_scheduler_job" "deploy_cron" {
  paused = false
  project = var.project_id
  region  = var.location
  name    = "deploy-cron"

  schedule  = "*/50 * * * *"
  time_zone = "America/Los_Angeles"

  attempt_deadline = "120s"

  // TODO(phboneff): use a batch job instead maybe
  http_target {
    http_method = "POST"
    uri         = "https://cloudbuild.googleapis.com/v1/projects/${var.project_id}/locations/${var.location}/triggers/${google_cloudbuild_trigger.build_trigger.trigger_id}:run"
    body        = base64encode(jsonencode({
      source = {
        branchName = "preloader"
      }
    }))
    headers = {
      "Content-Type" = "application/json"
    }

    oauth_token {
      service_account_email = local.scheduler_service_account
    }
  }
}