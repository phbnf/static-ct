terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//cloudbuild/preloader"
}

locals {
  env            = path_relative_to_include()
  project_id     = get_env("GOOGLE_PROJECT", "static-ct-staging")
  location       = get_env("GOOGLE_REGION", "us-central1")
  github_owner   = get_env("GITHUB_OWNER", "transparency-dev")
  submission_url = get_env("SUBMISSION_URL")
}

inputs = local

remote_state {
  backend = "gcs"

  config = {
    project  = local.project_id
    location = local.location
    bucket   = "${local.project_id}-cloudbuild-preloader-terraform-state"
    prefix   = "terraform.tfstate"

    gcs_bucket_labels = {
      name = "terraform_state"
      env  = "${local.env}"
    }
  }
}

