variable "chaptarr_url" {
  type        = string
  description = "Base URL of the disposable Chaptarr instance under test."
}

variable "chaptarr_api_key" {
  type        = string
  description = "API key of the disposable Chaptarr instance under test."
  sensitive   = true
}

variable "library_path" {
  type        = string
  description = "Loopback library path created by the replay harness. Docker uses the acceptance volume; native uses a temporary directory."
}

variable "ntfy_server_url" {
  type    = string
  default = "https://ntfy.example.invalid"
}

variable "ntfy_topic_media" {
  type    = string
  default = "replay-media"
}
