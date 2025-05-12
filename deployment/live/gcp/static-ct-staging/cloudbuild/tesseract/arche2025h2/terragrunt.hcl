terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//cloudbuild/tesseract"
}

locals {
  docker_env     = include.root.locals.base_name
}

include "root" {
  path   = find_in_parent_folders()
  expose = true
}

inputs = merge(
  local,
  include.root.locals
)

