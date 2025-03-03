#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Check if version argument is provided
if [ $# -ne 1 ]; then
    echo -e "${RED}Error: Version number is required${NC}"
    echo "Usage: $0 <version>"
    echo "Example: $0 1.0.0"
    exit 1
fi

VERSION=$1

# Validate version format (should be numbers separated by dots)
if ! [[ $VERSION =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo -e "${RED}Error: Invalid version format${NC}"
    echo "Version should be in format: X.Y.Z (e.g., 1.0.0)"
    exit 1
fi

# Get the current date
DATE=$(date +'%Y-%m-%d')

echo -e "${BLUE}Creating release v${VERSION}${NC}"

# Get the root directory of the project
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHANGELOG_PATH="${ROOT_DIR}/CHANGELOG.md"

# Check if CHANGELOG.md exists
if [ ! -f "$CHANGELOG_PATH" ]; then
    echo -e "${RED}Error: CHANGELOG.md not found at: $CHANGELOG_PATH${NC}"
    exit 1
fi

# Update CHANGELOG.md
echo -e "${BLUE}Updating CHANGELOG.md...${NC}"
if [ "$(uname)" = "Darwin" ]; then
    # macOS version of sed
    sed -i '' "s/## \[Unreleased\]/## [${VERSION}] - ${DATE}/" "$CHANGELOG_PATH"
else
    # Linux version of sed
    sed -i "s/## \[Unreleased\]/## [${VERSION}] - ${DATE}/" "$CHANGELOG_PATH"
fi

# Check if the change was successful
if ! grep -q "## \[${VERSION}\] - ${DATE}" "$CHANGELOG_PATH"; then
    echo -e "${RED}Error: Failed to update CHANGELOG.md${NC}"
    exit 1
fi

# Change to root directory for git operations
cd "$ROOT_DIR"

# Stage CHANGELOG.md
git add CHANGELOG.md

# Commit the change
echo -e "${BLUE}Committing CHANGELOG.md changes...${NC}"
git commit -m "chore: update CHANGELOG.md for v${VERSION}"

# Create and push tag
echo -e "${BLUE}Creating and pushing tag v${VERSION}...${NC}"
git tag "v${VERSION}"
git push origin "v${VERSION}"
git push origin main

echo -e "${GREEN}Release v${VERSION} created successfully!${NC}"
echo -e "${YELLOW}The GitHub Action workflow will now:${NC}"
echo "1. Build the binaries for all platforms"
echo "2. Create a GitHub release"
echo "3. Build and push the Docker image"
echo -e "\n${BLUE}You can monitor the progress at:${NC}"
echo "https://github.com/inferoute/inferoute-client/actions"
