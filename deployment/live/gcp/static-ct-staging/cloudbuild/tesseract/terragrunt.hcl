locals {
  env            = get_env("GOOGLE_ENV", "staging")
  project_id     = get_env("GOOGLE_PROJECT", "static-ct-staging")
  location       = get_env("GOOGLE_REGION", "us-central1")
  base_name      = path_relative_to_include()
  github_owner   = get_env("GITHUB_OWNER", "transparency-dev")
  log_terragrunt = "deployment/live/gcp/static-ct-staging/logs/${local.base_name}"
}

remote_state {
  backend = "gcs"

  config = {
    project  = local.project_id
    location = local.location
    bucket   = "${local.project_id}-cloudbuild-${local.base_name}-terraform-state"
    prefix   = "terraform.tfstate"

    gcs_bucket_labels = {
      name = "terraform_state"
      env  = "${local.env}"
    }
  }
}

