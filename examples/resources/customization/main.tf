variable "proxy_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "metadata_api_key" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "chaptarr_tag" "managed" {
  label = "managed"
}

data "chaptarr_tag_details" "all" {}

data "chaptarr_custom_format_schema" "current" {}

resource "chaptarr_custom_format" "preferred" {
  name                  = "Preferred release"
  applies_to            = "both"
  include_when_renaming = true
  specifications_json = jsonencode([
    {
      name           = "Release title"
      implementation = "ReleaseTitleSpecification"
      negate         = false
      required       = false
      fields = [{
        name  = "value"
        value = "preferred"
      }]
    }
  ])
}

resource "chaptarr_custom_filter" "monitored" {
  type  = "author"
  label = "Monitored authors"
  filters_json = jsonencode([
    {
      key      = "monitored"
      operator = "equal"
      value    = true
    }
  ])
}

resource "chaptarr_proxy" "outbound" {
  name     = "Managed proxy"
  type     = "http"
  hostname = "proxy.example.test"
  port     = 8080
  username = "chaptarr"
  password = var.proxy_password
}

data "chaptarr_metadata_schema" "current" {}

# Match implementation, config_contract, and field names to the current
# metadata schema. Protected fields belong only in secret_fields.
resource "chaptarr_metadata" "library" {
  name            = "Managed metadata"
  implementation  = "ExampleMetadataProvider"
  config_contract = "ExampleMetadataSettings"
  enable          = true
  tags            = [tonumber(chaptarr_tag.managed.id)]

  field_values_json = jsonencode({
    baseUrl = "https://metadata.example.test"
  })

  secret_fields = {
    apiKey = var.metadata_api_key
  }
}
