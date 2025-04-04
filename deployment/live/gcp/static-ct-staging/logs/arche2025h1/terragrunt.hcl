terraform {
  source = "${get_repo_root()}/deployment/modules/gcp//tesseract/conformance"
}

locals {
  env                    = include.root.locals.env
  docker_env             = local.env
  base_name              = include.root.locals.base_name
  origin_suffix          = include.root.locals.origin_suffix
  server_docker_image    = "${include.root.locals.location}-docker.pkg.dev/${include.root.locals.project_id}/docker-${local.env}/conformance-gcp:${include.root.locals.docker_container_tag}"
  preloader_docker_image = "${include.root.locals.location}-docker.pkg.dev/${include.root.locals.project_id}/docker-${local.env}/preloader:${include.root.locals.docker_container_tag}"
}

include "root" {
  path   = find_in_parent_folders()
  expose = true
}

inputs = merge(
  local,
  include.root.locals,
)
