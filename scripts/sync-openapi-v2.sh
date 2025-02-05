#!/usr/bin/env bash
set -eu -o pipefail

pushd "$HOME/stripe/zoolander"

# Pull master.
echo "Bringing master up to date."
git checkout master && git pull

# Grab SHA so we can save this to a file for some kind of "paper trail".
SHA=$(git rev-parse HEAD)

echo "⏳ Retrieving v2 openapi spec..."

./scripts/api-services/apiv2 apispecdump --version="2025-01-27.acacia" --format OPENAPI_JSON --variant SDK --out-file spec3.v2.sdk.json

popd

rm -f api/openapi-spec/spec3.v2.sdk.json

echo "$SHA" > api/ZOOLANDER_SHA

cp ~/stripe/zoolander/spec3.v2.sdk.json api/openapi-spec/

echo "⏳ Generating resource commands..."

make build

echo "✅ Successfully generated resource commands and rebuilt CLI."

