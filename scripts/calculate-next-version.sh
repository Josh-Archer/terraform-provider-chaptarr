#!/usr/bin/env bash
set -euo pipefail

# Allow manual override tag from workflow_dispatch
OVERRIDE_TAG="${1:-}"

if [[ -n "${OVERRIDE_TAG}" ]]; then
  echo "Using explicit override tag: ${OVERRIDE_TAG}"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    echo "should_release=true" >> "${GITHUB_OUTPUT}"
    echo "next_tag=${OVERRIDE_TAG}" >> "${GITHUB_OUTPUT}"
  fi
  exit 0
fi

# Fetch tags from remote
git fetch --tags --force origin 2>/dev/null || true

# Find latest v-prefixed SemVer tag
LATEST_TAG=$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n 1 || true)
if [[ -z "${LATEST_TAG}" ]]; then
  LATEST_TAG="v0.0.0"
fi

echo "Latest version tag: ${LATEST_TAG}"

CLEAN_VER="${LATEST_TAG#v}"
IFS='.' read -r MAJOR MINOR PATCH <<< "${CLEAN_VER}"

# Check commits since the latest tag
if [[ "${LATEST_TAG}" != "v0.0.0" ]]; then
  COMMITS=$(git log "${LATEST_TAG}..HEAD" --oneline 2>/dev/null || true)
else
  COMMITS=$(git log --oneline 2>/dev/null || true)
fi

if [[ -z "${COMMITS}" ]]; then
  echo "No new commits since ${LATEST_TAG}. Skipping release."
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    echo "should_release=false" >> "${GITHUB_OUTPUT}"
  fi
  exit 0
fi

echo "Commits since ${LATEST_TAG}:"
echo "${COMMITS}"

# Determine bump type based on Conventional Commits & Branch/PR naming patterns
BUMP="patch"

# Check for breaking change / Major bump (!: or BREAKING CHANGE)
if printf '%s\n' "${COMMITS}" | grep -Ei '(!:|[[:space:]]![[:space:]]|BREAKING[ -]CHANGE)' >/dev/null 2>&1; then
  BUMP="major"
# Check for feature / Minor bump (feat:, feature/, scope-based features)
elif printf '%s\n' "${COMMITS}" | grep -Ei '(^|[^a-zA-Z0-9])(feat|feature|minor|datasources|operations|profiles|storage|customization|integrations|import-lists|authors|books)(:|\(|/|\b)' >/dev/null 2>&1; then
  BUMP="minor"
fi

if [[ "${BUMP}" == "major" ]]; then
  NEXT_MAJOR=$((MAJOR + 1))
  NEXT_TAG="v${NEXT_MAJOR}.0.0"
elif [[ "${BUMP}" == "minor" ]]; then
  NEXT_MINOR=$((MINOR + 1))
  NEXT_TAG="v${MAJOR}.${NEXT_MINOR}.0"
else
  NEXT_PATCH=$((PATCH + 1))
  NEXT_TAG="v${MAJOR}.${MINOR}.${NEXT_PATCH}"
fi

echo "Calculated bump: ${BUMP} -> ${NEXT_TAG}"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  echo "should_release=true" >> "${GITHUB_OUTPUT}"
  echo "next_tag=${NEXT_TAG}" >> "${GITHUB_OUTPUT}"
fi
