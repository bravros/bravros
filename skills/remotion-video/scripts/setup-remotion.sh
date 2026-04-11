#!/bin/bash
# Remotion Video Project Setup Script
# Checks for Remotion installation, installs if needed, and verifies environment

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo "🎬 Remotion Video Setup"
echo "━━━━━━━━━━━━━━━━━━━━━━"

# Check if we're in a project directory
if [ ! -f "package.json" ]; then
    echo -e "${YELLOW}No package.json found. Creating a new Remotion project...${NC}"
    npx create-video@latest --blank
    echo -e "${GREEN}✓ Remotion project created${NC}"
    cd "$(ls -td */ | head -1)" # Enter the newly created directory
fi

# Check if Remotion is installed
if grep -q '"remotion"' package.json 2>/dev/null; then
    echo -e "${GREEN}✓ Remotion is installed${NC}"
else
    echo -e "${YELLOW}Installing Remotion...${NC}"
    npm install remotion @remotion/cli @remotion/transitions @remotion/captions
    echo -e "${GREEN}✓ Remotion installed${NC}"
fi

# Check for essential Remotion packages
PACKAGES=("@remotion/transitions" "@remotion/captions" "@remotion/light-leaks")
for pkg in "${PACKAGES[@]}"; do
    if grep -q "\"$pkg\"" package.json 2>/dev/null; then
        echo -e "${GREEN}✓ $pkg installed${NC}"
    else
        echo -e "${YELLOW}Installing $pkg...${NC}"
        npx remotion add "$pkg" 2>/dev/null || npm install "$pkg"
        echo -e "${GREEN}✓ $pkg installed${NC}"
    fi
done

# Check for TailwindCSS
if grep -q "tailwindcss" package.json 2>/dev/null; then
    echo -e "${GREEN}✓ TailwindCSS installed${NC}"
else
    echo -e "${YELLOW}TailwindCSS not found — recommended for rapid styling${NC}"
    echo "  Run: npm install -D tailwindcss @tailwindcss/vite"
fi

# Create standard directory structure
echo ""
echo "📁 Setting up directory structure..."
mkdir -p src public/images public/audio public/voiceover public/fonts public/badges
echo -e "${GREEN}✓ Directory structure ready${NC}"

# Check for remotion-best-practices skill
echo ""
echo "🔍 Checking for remotion-best-practices skill..."
if [ -d ".claude/skills/remotion-best-practices" ] || [ -d "$HOME/.claude/skills/remotion-best-practices" ]; then
    echo -e "${GREEN}✓ remotion-best-practices skill found${NC}"
else
    echo -e "${YELLOW}Installing remotion-best-practices skill...${NC}"
    npx skills add remotion-dev/skills --skill remotion-best-practices -y 2>/dev/null || echo -e "${YELLOW}Could not auto-install — install manually: npx skills add remotion-dev/skills${NC}"
fi

# Verify Node.js
echo ""
echo "🔍 Environment check..."
NODE_VERSION=$(node -v 2>/dev/null || echo "not found")
echo "  Node.js: $NODE_VERSION"
NPM_VERSION=$(npm -v 2>/dev/null || echo "not found")
echo "  npm: $NPM_VERSION"

# Check for FFmpeg (optional but useful)
if command -v ffmpeg &>/dev/null; then
    FFMPEG_VERSION=$(ffmpeg -version 2>/dev/null | head -1 | cut -d' ' -f3)
    echo -e "  FFmpeg: ${GREEN}$FFMPEG_VERSION${NC}"
else
    echo -e "  FFmpeg: ${YELLOW}not found (optional — needed for video trimming/silence detection)${NC}"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${GREEN}🎬 Remotion is ready!${NC}"
echo ""
echo "Next steps:"
echo "  1. Run 'npm run dev' in a separate terminal to start Remotion Studio"
echo "  2. Use the 'plan-video' command to start creating your first video"
echo "  3. Open http://localhost:3000 to preview"
