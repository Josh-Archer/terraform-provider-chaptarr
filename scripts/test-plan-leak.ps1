$ErrorActionPreference = 'Stop'

if (-not (Get-Command tofu -ErrorAction SilentlyContinue)) {
    throw 'OpenTofu is required for the provider plan leak smoke test.'
}

$repositoryRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$systemTemp = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$testRoot = Join-Path $systemTemp ("chaptarr-plan-" + [Guid]::NewGuid().ToString('N'))
$resolvedTestRoot = [IO.Path]::GetFullPath($testRoot)
if (-not $resolvedTestRoot.StartsWith($systemTemp, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Refusing to use a test directory outside the system temporary directory.'
}

$environmentNames = @('CHAPTARR_API_KEY', 'TF_VAR_oidc_client_secret', 'TF_VAR_calibre_password', 'TF_VAR_proxy_password', 'TF_VAR_metadata_api_key', 'TF_VAR_integration_token', 'TF_VAR_hardcover_token', 'TF_VAR_postgres_admin_password', 'TF_VAR_postgres_bridge_token', 'TF_VAR_postgres_secret_key', 'TF_VAR_postgres_role_password', 'TF_CLI_CONFIG_FILE', 'TF_LOG', 'TF_LOG_PATH', 'GOTOOLCHAIN')
$previousEnvironment = @{}
foreach ($name in $environmentNames) {
    $item = Get-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue
    if ($null -ne $item) {
        $previousEnvironment[$name] = @{ Present = $true; Value = $item.Value }
    }
    else {
        $previousEnvironment[$name] = @{ Present = $false; Value = $null }
    }
}

try {
    if (-not $env:GOTOOLCHAIN) {
        $env:GOTOOLCHAIN = 'go1.25.12+auto'
    }
    $binaryDirectory = Join-Path $resolvedTestRoot 'bin'
    $configurationDirectory = Join-Path $resolvedTestRoot 'configuration'
    New-Item -ItemType Directory -Force -Path $binaryDirectory, $configurationDirectory | Out-Null

    $providerBinary = Join-Path $binaryDirectory 'terraform-provider-chaptarr.exe'
    & go build -trimpath -o $providerBinary $repositoryRoot
    if ($LASTEXITCODE -ne 0) {
        throw 'Provider build failed.'
    }

    $binaryDirectoryHCL = $binaryDirectory.Replace('\', '/')
    @"
provider_installation {
  dev_overrides {
    "josh-archer/chaptarr" = "$binaryDirectoryHCL"
  }
  direct {}
}
"@ | Set-Content -Encoding ascii (Join-Path $resolvedTestRoot 'tofurc')

    @'
terraform {

  required_version = ">= 1.11.2"

  required_providers {
    chaptarr = {
      source = "josh-archer/chaptarr"
    }
  }
}

provider "chaptarr" {
  url = "https://chaptarr.example.test/reverse-proxy"
}

variable "oidc_client_secret" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "calibre_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

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

variable "integration_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "hardcover_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "postgres_admin_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "postgres_bridge_token" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "postgres_secret_key" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "postgres_role_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "chaptarr_host_config" "leak_test" {
  instance_name      = "plan-leak-test"
  oidc_client_secret = var.oidc_client_secret
}

resource "chaptarr_root_folder" "calibre" {
  name               = "Plan leak fixture"
  path               = "/library/plan-leak-fixture"
  folder_type        = "ebook"
  is_calibre_library = true
  host               = "calibre.example.test"
  port               = 8080
  library            = "fixture"
  output_profile     = "default"
  username           = "fixture"
  password           = var.calibre_password
}

resource "chaptarr_proxy" "fixture" {
  name     = "Plan leak proxy"
  type     = "http"
  hostname = "proxy.example.test"
  port     = 8080
  username = "fixture"
  password = var.proxy_password
}

resource "chaptarr_metadata" "fixture" {
  name              = "Plan leak metadata"
  implementation    = "FixtureMetadataProvider"
  config_contract   = "FixtureMetadataSettings"
  enable            = false
  field_values_json = jsonencode({ baseUrl = "https://metadata.example.test" })
  secret_fields     = { apiKey = var.metadata_api_key }
}

resource "chaptarr_indexer" "fixture" {
  name                      = "Plan leak indexer"
  implementation            = "FixtureIndexer"
  config_contract           = "FixtureIndexerSettings"
  enable                    = false
  enable_rss                = false
  enable_automatic_search   = false
  enable_interactive_search = false
  priority                  = 25
  field_values_json         = jsonencode({ baseUrl = "https://indexer.example.test" })
  secret_fields             = { apiToken = var.integration_token }
}

resource "chaptarr_hardcover_config" "fixture" {
  token                     = var.hardcover_token
  allow_external_validation = true
  observe_server            = false
}

resource "chaptarr_postgres_database" "fixture" {
  server_host             = "postgres.example.test"
  admin_username          = "fixture-admin"
  admin_password          = var.postgres_admin_password
  vaultwarden_bridge_url  = "https://bridge.example.test"
  vaultwarden_bridge_token = var.postgres_bridge_token
  vaultwarden_secret_key  = var.postgres_secret_key
  role_password           = var.postgres_role_password
}

resource "chaptarr_author" "fixture" {
  foreign_author_id          = "hc:191785"
  monitored                  = true
  audiobook_monitor_existing = 1
  audiobook_monitor_future   = false
  ebook_monitor_existing     = 0
  ebook_monitor_future       = false
  audiobook_root_folder_path    = "/audiobooks"
  audiobook_quality_profile_id  = 2
  audiobook_metadata_profile_id = 3
  search_for_missing_books = false
  move_files_on_update     = false
  delete_files_on_destroy  = false
}

resource "chaptarr_series" "fixture" {
  foreign_series_id   = "hc:series-200"
  media_type          = "audiobook"
  monitor_existing    = "select"
  monitor_future      = false
  root_folder_path    = "/audiobooks"
  quality_profile_id  = 2
  metadata_profile_id = 3
  selected_books = [{
    foreign_book_id   = "hc:book-201"
    foreign_author_id = "hc:191785"
  }]
}

resource "chaptarr_book" "fixture" {
  lookup_json = jsonencode({
    foreignBookId = "hc:11"
    author        = { id = 7 }
    editions = [{
      id               = 61
      foreignEditionId = "hc:edition:22"
      monitored        = true
    }]
  })
  foreign_book_id       = "hc:11"
  author_id             = 7
  media_type            = "audiobook"
  monitored             = true
  any_edition_ok        = false
  monitored_edition_id  = "hc:edition:22"
  search_for_new_book   = false
  delete_files_on_destroy = false
}

resource "chaptarr_edition" "fixture" {
  book_id    = 51
  edition_id = 61
  monitored  = true
}
'@ | Set-Content -Encoding ascii (Join-Path $configurationDirectory 'main.tf')

    $sentinel = 'CHAPTARR_TEST_API_KEY_SENTINEL_DO_NOT_USE_79f6f1d2'
	$hostSentinel = 'CHAPTARR_HOST_WRITE_ONLY_SENTINEL_DO_NOT_USE_913ad7c4'
	$calibreSentinel = 'CHAPTARR_TEST_CALIBRE_PASSWORD_SENTINEL_DO_NOT_USE_3b4e911c'
	$proxySentinel = 'CHAPTARR_TEST_PROXY_PASSWORD_SENTINEL_DO_NOT_USE_46b2c884'
	$metadataSentinel = 'CHAPTARR_TEST_METADATA_API_KEY_SENTINEL_DO_NOT_USE_8d187ab2'
	$integrationSentinel = 'CHAPTARR_TEST_INTEGRATION_TOKEN_SENTINEL_DO_NOT_USE_91dc63af'
	$hardcoverSentinel = 'CHAPTARR_TEST_HARDCOVER_TOKEN_SENTINEL_DO_NOT_USE_14ce27ab'
	$postgresAdminSentinel = 'CHAPTARR_TEST_POSTGRES_ADMIN_SENTINEL_DO_NOT_USE_552c9d1a'
	$postgresBridgeSentinel = 'CHAPTARR_TEST_POSTGRES_BRIDGE_SENTINEL_DO_NOT_USE_42ec9b17'
	$postgresSecretKeySentinel = 'CHAPTARR_TEST_POSTGRES_SECRET_KEY_SENTINEL_DO_NOT_USE_a7dc3128'
	$postgresRoleSentinel = 'CHAPTARR_TEST_POSTGRES_ROLE_SENTINEL_DO_NOT_USE_5ec94d72'
    $env:CHAPTARR_API_KEY = $sentinel
	$env:TF_VAR_oidc_client_secret = $hostSentinel
	$env:TF_VAR_calibre_password = $calibreSentinel
	$env:TF_VAR_proxy_password = $proxySentinel
	$env:TF_VAR_metadata_api_key = $metadataSentinel
	$env:TF_VAR_integration_token = $integrationSentinel
	$env:TF_VAR_hardcover_token = $hardcoverSentinel
	$env:TF_VAR_postgres_admin_password = $postgresAdminSentinel
	$env:TF_VAR_postgres_bridge_token = $postgresBridgeSentinel
	$env:TF_VAR_postgres_secret_key = $postgresSecretKeySentinel
	$env:TF_VAR_postgres_role_password = $postgresRoleSentinel
    $env:TF_CLI_CONFIG_FILE = Join-Path $resolvedTestRoot 'tofurc'
    Remove-Item Env:TF_LOG -ErrorAction SilentlyContinue
    Remove-Item Env:TF_LOG_PATH -ErrorAction SilentlyContinue

    $planPath = Join-Path $resolvedTestRoot 'plan.tfplan'
    $planOutput = Join-Path $resolvedTestRoot 'plan-output.txt'
    $savedErrorPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & tofu "-chdir=$configurationDirectory" plan -input=false -no-color "-out=$planPath" *> $planOutput
    $planStatus = $LASTEXITCODE
    $ErrorActionPreference = $savedErrorPreference
    if ($planStatus -ne 0) {
        Get-Content $planOutput
        throw 'OpenTofu plan smoke test failed.'
    }

    $showOutput = Join-Path $resolvedTestRoot 'plan.json'
    $ErrorActionPreference = 'Continue'
    & tofu "-chdir=$configurationDirectory" show -json $planPath *> $showOutput
    $showStatus = $LASTEXITCODE
    $ErrorActionPreference = $savedErrorPreference
    if ($showStatus -ne 0) {
        throw 'OpenTofu plan JSON rendering failed.'
    }

    foreach ($path in @($planOutput, $showOutput)) {
		$content = Get-Content -Raw $path
		foreach ($secret in @($sentinel, $hostSentinel, $calibreSentinel, $proxySentinel, $metadataSentinel, $integrationSentinel, $hardcoverSentinel, $postgresAdminSentinel, $postgresBridgeSentinel, $postgresSecretKeySentinel, $postgresRoleSentinel)) {
			if ($content.Contains($secret)) {
				throw "Synthetic credential leaked into $(Split-Path -Leaf $path)."
			}
		}
	}

	Write-Output 'OpenTofu plan output contains no synthetic credentials.'
}
finally {
    foreach ($name in $environmentNames) {
        if ($previousEnvironment[$name].Present) {
            [Environment]::SetEnvironmentVariable($name, $previousEnvironment[$name].Value, 'Process')
        }
        else {
            Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue
        }
    }
    if (Test-Path -LiteralPath $resolvedTestRoot) {
        Remove-Item -LiteralPath $resolvedTestRoot -Recurse -Force
    }
}
