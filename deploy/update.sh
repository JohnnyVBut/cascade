#!/bin/bash
set -e

cd "$(dirname "$0")/.."

echo "Pulling latest code..."
git pull

echo "Building new image..."
./build.sh

echo "Restarting container..."
docker compose down
docker compose up -d

echo "Done. Check logs: docker logs cascade"
