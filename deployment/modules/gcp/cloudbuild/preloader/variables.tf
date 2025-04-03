variable "project_id" {
  description = "GCP project ID where the log is hosted"
  type        = string
}

variable "location" {
  description = "Location in which to create resources"
  type        = string
}

variable "env" {
  description = "Unique identifier for the env, e.g. dev or ci or prod"
  type        = string
}

variable "github_owner" {
  description = "GitHub owner used in Cloud Build trigger repository mapping"
  type        = string
}

variable "submission_url" {
  description = "Submission URL of the log to write to"
  type        = string
}