#!/bin/sh
set -e

git pull
docker compose pull
docker compose build agent

echo "praktor upgraded to latest version. run docker compose up -d to (re)start"
