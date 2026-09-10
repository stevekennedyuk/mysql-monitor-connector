#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "install.sh must run as root" >&2
  exit 1
fi

binary=${1:-./mysql-monitor-connector}
test -f "$binary"

if ! getent group mysql-monitor >/dev/null 2>&1; then
  groupadd --system mysql-monitor
fi
if ! id mysql-monitor >/dev/null 2>&1; then
  useradd --system --gid mysql-monitor --home-dir /var/lib/mysql-monitor-connector --shell /usr/sbin/nologin mysql-monitor
fi

install -m 0755 "$binary" /usr/local/bin/mysql-monitor-connector
install -d -m 0750 -o root -g mysql-monitor /etc/mysql-monitor-connector
install -d -m 0750 -o mysql-monitor -g mysql-monitor /var/lib/mysql-monitor-connector
install -m 0644 packaging/mysql-monitor-connector.service /etc/systemd/system/mysql-monitor-connector.service
systemctl daemon-reload

echo "Installed. Enroll the connector, add a target, then run:"
echo "  systemctl enable --now mysql-monitor-connector"
