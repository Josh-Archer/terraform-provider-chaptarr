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

$environmentNames = @('CHAPTARR_API_KEY', 'TF_CLI_CONFIG_FILE', 'TF_LOG', 'TF_LOG_PATH', 'GOTOOLCHAIN')
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
  required_providers {
    chaptarr = {
      source = "josh-archer/chaptarr"
    }
  }
}

provider "chaptarr" {
  url = "https://chaptarr.example.test/reverse-proxy"
}
'@ | Set-Content -Encoding ascii (Join-Path $configurationDirectory 'main.tf')

    $sentinel = 'CHAPTARR_TEST_API_KEY_SENTINEL_DO_NOT_USE_79f6f1d2'
    $env:CHAPTARR_API_KEY = $sentinel
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
        if ((Get-Content -Raw $path).Contains($sentinel)) {
            throw "Synthetic API key leaked into $(Split-Path -Leaf $path)."
        }
    }

    Write-Output 'OpenTofu plan output contains no synthetic API key.'
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
