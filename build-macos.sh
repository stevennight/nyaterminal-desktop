#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

MODE="build"
PLATFORM=""
CREATE_DMG=0
CREATE_ZIP=0
VERSION="0.1.0-dev"
COMMIT=""
BUILD_DATE=""
UPDATE_REPOSITORY="nyaterminal/nyaterminal-desktop"
SIGN_IDENTITY="-"

usage() {
  cat <<'EOF'
Usage: ./build-macos.sh [options]

Options:
  --mode <dev|build>             Build mode (default: build)
  --platform <darwin/...>        Target platform: darwin/arm64, darwin/amd64, or darwin/universal
                                 (default: current macOS architecture)
  --dmg                          Package the built .app into a .dmg disk image
  --zip                          Package the built .app into a .zip archive
  --version <version>            Application version (default: 0.1.0-dev)
  --commit <sha>                 Git commit SHA to embed
  --build-date <rfc3339>         UTC build timestamp (default: current UTC time)
  --update-repository <o/r>      GitHub update repository (default: nyaterminal/nyaterminal-desktop)
  --sign-identity <identity>     codesign identity (default: "-" for ad-hoc signing; use "none" to skip)
  -h, --help                     Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)
      MODE="${2:-}"
      shift 2
      ;;
    --platform)
      PLATFORM="${2:-}"
      shift 2
      ;;
    --dmg)
      CREATE_DMG=1
      shift
      ;;
    --zip)
      CREATE_ZIP=1
      shift
      ;;
    --version)
      VERSION="${2:-}"
      shift 2
      ;;
    --commit)
      COMMIT="${2:-}"
      shift 2
      ;;
    --build-date)
      BUILD_DATE="${2:-}"
      shift 2
      ;;
    --update-repository)
      UPDATE_REPOSITORY="${2:-}"
      shift 2
      ;;
    --sign-identity)
      SIGN_IDENTITY="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "build-macos.sh must be run on macOS." >&2
  exit 1
fi

if [[ "${MODE}" != "dev" && "${MODE}" != "build" ]]; then
  echo "--mode must be 'dev' or 'build'." >&2
  exit 1
fi

if [[ "${MODE}" == "dev" && ( "${CREATE_DMG}" -eq 1 || "${CREATE_ZIP}" -eq 1 ) ]]; then
  echo "--dmg and --zip can only be used with --mode build." >&2
  exit 1
fi

if [[ -z "${PLATFORM}" ]]; then
  case "$(uname -m)" in
    arm64) PLATFORM="darwin/arm64" ;;
    x86_64) PLATFORM="darwin/amd64" ;;
    *)
      echo "Unsupported macOS architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
fi

case "${PLATFORM}" in
  darwin/arm64|darwin/amd64|darwin/universal) ;;
  *)
    echo "Unsupported --platform value: ${PLATFORM}. Expected darwin/arm64, darwin/amd64, or darwin/universal." >&2
    exit 1
    ;;
esac

require_cmd() {
  local name="$1"
  local hint="$2"
  if ! command -v "${name}" >/dev/null 2>&1; then
    echo "${name} was not found. ${hint}" >&2
    exit 1
  fi
}

require_cmd go "Install Go 1.26+ and add it to PATH."
require_cmd npm "Install Node.js 22+ and add it to PATH."

ICON_SOURCE="${SCRIPT_DIR}/frontend/public/nyaterminal-icon-dark-gray.png"
PLIST_TEMPLATE="${SCRIPT_DIR}/installer/macos/Info.plist"
if [[ ! -f "${ICON_SOURCE}" ]]; then
  echo "Application icon source is missing at ${ICON_SOURCE}." >&2
  exit 1
fi
if [[ ! -f "${PLIST_TEMPLATE}" ]]; then
  echo "macOS Info.plist template is missing at ${PLIST_TEMPLATE}." >&2
  exit 1
fi

mkdir -p "${SCRIPT_DIR}/build/darwin"
cp -f "${ICON_SOURCE}" "${SCRIPT_DIR}/build/appicon.png"
cp -f "${PLIST_TEMPLATE}" "${SCRIPT_DIR}/build/darwin/Info.plist"
cp -f "${PLIST_TEMPLATE}" "${SCRIPT_DIR}/build/darwin/Info.dev.plist"

REQUIRED_WAILS_VERSION="v2.12.0"
GOBIN="$(go env GOPATH)/bin"
export PATH="${GOBIN}:${PATH}"

INSTALL_WAILS=0
if ! command -v wails >/dev/null 2>&1; then
  INSTALL_WAILS=1
else
  INSTALLED_WAILS_VERSION="$(wails version 2>&1 || true)"
  if [[ "${INSTALLED_WAILS_VERSION}" != *"${REQUIRED_WAILS_VERSION}"* ]]; then
    INSTALL_WAILS=1
  fi
fi

if [[ "${INSTALL_WAILS}" -eq 1 ]]; then
  echo "Installing Wails CLI ${REQUIRED_WAILS_VERSION}..."
  go install "github.com/wailsapp/wails/v2/cmd/wails@${REQUIRED_WAILS_VERSION}"
fi

PRODUCT_VERSION="${VERSION#v}"
STABLE_SEMVER_REGEX='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
IS_STABLE_VERSION=0
if [[ "${PRODUCT_VERSION}" =~ ${STABLE_SEMVER_REGEX} ]]; then
  IS_STABLE_VERSION=1
  VERSION="${PRODUCT_VERSION}"
fi

if [[ -z "${BUILD_DATE}" ]]; then
  BUILD_DATE="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
fi

export NYATERMINAL_DESKTOP_VERSION="${VERSION}"
export NYATERMINAL_DESKTOP_COMMIT="${COMMIT}"
export NYATERMINAL_DESKTOP_BUILD_DATE="${BUILD_DATE}"
export NYATERMINAL_UPDATE_REPOSITORY="${UPDATE_REPOSITORY}"

(
  cd "${SCRIPT_DIR}/frontend"
  if [[ ! -d node_modules ]]; then
    npm install
  fi
  npm run build
)

if [[ "${MODE}" == "dev" ]]; then
  CGO_ENABLED=1 wails dev
  exit $?
fi

WAILS_CONFIG="${SCRIPT_DIR}/wails.json"
WAILS_CONFIG_BACKUP=""
cleanup() {
  if [[ -n "${WAILS_CONFIG_BACKUP}" && -f "${WAILS_CONFIG_BACKUP}" ]]; then
    mv -f "${WAILS_CONFIG_BACKUP}" "${WAILS_CONFIG}"
  fi
}
trap cleanup EXIT

if [[ "${IS_STABLE_VERSION}" -eq 1 ]]; then
  WAILS_CONFIG_BACKUP="$(mktemp)"
  cp -f "${WAILS_CONFIG}" "${WAILS_CONFIG_BACKUP}"
  node -e '
    const fs = require("fs");
    const [path, version] = process.argv.slice(1);
    const config = JSON.parse(fs.readFileSync(path, "utf8"));
    config.info = config.info || {};
    config.info.productVersion = version;
    fs.writeFileSync(path, JSON.stringify(config, null, 2) + "\n", "utf8");
  ' "${WAILS_CONFIG}" "${PRODUCT_VERSION}"
fi

LDFLAGS="-X github.com/nyaterminal/nyaterminal-desktop/internal/version.Version=${VERSION} \
-X github.com/nyaterminal/nyaterminal-desktop/internal/version.Commit=${COMMIT} \
-X github.com/nyaterminal/nyaterminal-desktop/internal/version.BuildDate=${BUILD_DATE} \
-X github.com/nyaterminal/nyaterminal-desktop/internal/version.UpdateRepository=${UPDATE_REPOSITORY}"

CGO_ENABLED=1 wails build -clean -trimpath -platform "${PLATFORM}" -ldflags "${LDFLAGS}"

APP_PATH="${SCRIPT_DIR}/build/bin/NyaTerminal.app"
if [[ ! -d "${APP_PATH}" ]]; then
  echo "Wails did not produce the expected app bundle at ${APP_PATH}." >&2
  exit 1
fi

if [[ "${SIGN_IDENTITY}" != "none" ]]; then
  echo "Signing ${APP_PATH} with identity '${SIGN_IDENTITY}'..."
  codesign --force --deep --sign "${SIGN_IDENTITY}" "${APP_PATH}"
fi

ARCH_LABEL="${PLATFORM#darwin/}"
if [[ "${CREATE_DMG}" -eq 1 ]]; then
  DMG_PATH="${SCRIPT_DIR}/build/bin/NyaTerminal_${VERSION}_macos_${ARCH_LABEL}.dmg"
  DMG_STAGE="$(mktemp -d)"
  cp -R "${APP_PATH}" "${DMG_STAGE}/NyaTerminal.app"
  ln -s /Applications "${DMG_STAGE}/Applications"
  rm -f "${DMG_PATH}"
  hdiutil create \
    -volname "NyaTerminal" \
    -srcfolder "${DMG_STAGE}" \
    -ov \
    -format UDZO \
    "${DMG_PATH}"
  rm -rf "${DMG_STAGE}"
  echo "Created DMG: ${DMG_PATH}"
fi

if [[ "${CREATE_ZIP}" -eq 1 ]]; then
  ZIP_PATH="${SCRIPT_DIR}/build/bin/NyaTerminal_${VERSION}_macos_${ARCH_LABEL}.zip"
  rm -f "${ZIP_PATH}"
  ditto -c -k --sequesterRsrc --keepParent "${APP_PATH}" "${ZIP_PATH}"
  echo "Created ZIP: ${ZIP_PATH}"
fi
