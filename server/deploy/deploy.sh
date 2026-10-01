#!/bin/bash
set -e

# ==============================================================================
# NetTurn VPS Auto-Deploy Script
# ==============================================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}"
echo "  _   _      _  _____                 "
echo " | \ | | ___| ||_   _|   _ _ __ _ __  "
echo " |  \| |/ _ \ __|| || | | | '__| '_ \ "
echo " | |\  |  __/ |_ | || |_| | |  | | | |"
echo " |_| \_|\___|\__||_| \__,_|_|  |_| |_|"
echo " NetTurn VPS Server Auto-Installer"
echo -e "${NC}"

if [ "$(id -u)" != "0" ]; then
   echo -e "${RED}[!] Этот скрипт должен быть запущен с правами root (sudo)${NC}" 1>&2
   exit 1
fi

echo -e "${GREEN}[*] Шаг 1: Проверка и установка системных зависимостей...${NC}"
if command -v apt-get >/dev/null 2>&1; then
    apt-get update -y
    apt-get install -y curl iptables tar golang-go
elif command -v dnf >/dev/null 2>&1; then
    dnf install -y curl iptables tar golang
elif command -v pacman >/dev/null 2>&1; then
    pacman -Sy --noconfirm curl iptables tar go
fi

PUBLIC_IP=$(curl -s4 https://api.ipify.org || curl -s4 https://ifconfig.me || echo "YOUR_VPS_IP")

PASSWORD=${PASSWORD:-}
if [ -z "$PASSWORD" ]; then
    PASSWORD=$(head /dev/urandom | tr -dc A-Za-z0-9 | head -c 16)
fi

PORT=${PORT:-56003}

echo -e "${GREEN}[*] Шаг 2: Компиляция серверного бинарника netturn-server...${NC}"
mkdir -p /etc/netturn
TMP_BUILD=$(mktemp -d)
git clone https://github.com/Vokiry/NetTurn.git "$TMP_BUILD/netturn" || {
    echo -e "${YELLOW}[!] Git clone не удался, попытка локальной сборки...${NC}"
}

if [ -d "$TMP_BUILD/netturn" ]; then
    cd "$TMP_BUILD/netturn"
    go build -ldflags="-s -w" -o /usr/local/bin/netturn-server ./cmd/netturn-server
    cd /
    rm -rf "$TMP_BUILD"
fi

if [ ! -f /usr/local/bin/netturn-server ]; then
    echo -e "${RED}[!] Не удалось собрать /usr/local/bin/netturn-server${NC}"
    exit 1
fi
chmod +x /usr/local/bin/netturn-server

echo -e "${GREEN}[*] Шаг 3: Настройка окружения и systemd сервиса...${NC}"
cat <<EOF > /etc/netturn/netturn.env
LISTEN_ADDR=0.0.0.0:${PORT}
PASSWORD=${PASSWORD}
EOF
chmod 600 /etc/netturn/netturn.env

cat <<'EOF' > /etc/systemd/system/netturn.service
[Unit]
Description=NetTurn Next-Gen VK Calls TURN Media Server
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/etc/netturn
EnvironmentFile=/etc/netturn/netturn.env
ExecStart=/bin/sh -c 'exec /usr/local/bin/netturn-server -listen "${LISTEN_ADDR}" -password "${PASSWORD}" -tun netturn-raw'
Restart=always
RestartSec=3
LimitNOFILE=65536
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_RAW
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_RAW

[Install]
WantedBy=multi-user.target
EOF

echo -e "${GREEN}[*] Шаг 4: Настройка сетевого форвардинга и файрвола...${NC}"
sysctl -w net.ipv4.ip_forward=1 >/dev/null
if ! grep -q "net.ipv4.ip_forward=1" /etc/sysctl.conf 2>/dev/null; then
    echo "net.ipv4.ip_forward=1" >> /etc/sysctl.conf
fi

systemctl daemon-reload
systemctl enable netturn.service
systemctl restart netturn.service

echo -e "\n${GREEN}================================================================${NC}"
echo -e "${GREEN}[✓] Сервер NetTurn успешно установлен и запущен!${NC}"
echo -e "${GREEN}================================================================${NC}"
echo -e "Публичный IP сервера:  ${YELLOW}${PUBLIC_IP}${NC}"
echo -e "Порт подключения:      ${YELLOW}${PORT}${NC} (UDP)"
echo -e "Пароль аутентификации: ${YELLOW}${PASSWORD}${NC}"
echo -e "----------------------------------------------------------------"
echo -e "Для подключения с Linux Desktop выполните:"
echo -e "${BLUE}netturn-client -peer ${PUBLIC_IP}:${PORT} -password ${PASSWORD} -vk-link \"<VK_CALL_LINK>\"${NC}"
echo -e "${GREEN}================================================================${NC}"
