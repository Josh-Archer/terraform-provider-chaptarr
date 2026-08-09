terraform {
  required_providers {
    chaptarr = {
      source = "josh-archer/chaptarr"
    }
  }
}

# Set CHAPTARR_API_KEY in the process environment.
provider "chaptarr" {
  url = "https://chaptarr.example.test"
}
