#!/bin/sh
# Removes what setup.sh created.
docker rm -f web-frontend port-blocker 2>/dev/null
