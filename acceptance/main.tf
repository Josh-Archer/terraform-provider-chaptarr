terraform {
  required_version = ">= 1.11.2"

  required_providers {
    chaptarr = {
      source = "josh-archer/chaptarr"
    }
  }
}

provider "chaptarr" {}

variable "tag_label" {
  type = string
}

data "chaptarr_api_info" "smoke" {}

data "chaptarr_system_status" "smoke" {}

resource "chaptarr_tag" "acceptance" {
  label = var.tag_label
}

check "api_v1" {
  assert {
    condition     = data.chaptarr_api_info.smoke.current == "v1"
    error_message = "The disposable Chaptarr environment did not expose API v1."
  }
}

output "tag_id" {
  value = chaptarr_tag.acceptance.id
}

output "chaptarr_version" {
  value = data.chaptarr_system_status.smoke.version
}
