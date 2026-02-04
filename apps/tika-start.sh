#!/usr/bin/env bash

set -eu

TIKA_JAR="${SNAP}/opt/tika/tika-server.jar"
TIKA_PORT=9998

# Ensure the HOME directory exists for snap_daemon
mkdir -p "${HOME}"

# Start Tika server as snap_daemon
exec "${SNAP}"/usr/bin/setpriv \
    --clear-groups \
    --reuid snap_daemon \
    --regid snap_daemon -- \
    "${JAVA_HOME}/bin/java" \
    ${JAVA_OPTS:-} \
    -cp "${TIKA_JAR}" \
    org.apache.tika.server.core.TikaServerCli \
    -h 0.0.0.0 \
    -p "${TIKA_PORT}" \
    -noFork
