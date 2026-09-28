# NetTurn

<div align="center">

> **Высокопроизводительный туннель следующего поколения через распределённую WebRTC TURN-инфраструктуру медиа-серверов VK Calls.**

[![Go Version](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Linux Desktop](https://img.shields.io/badge/Platform-Linux%20Desktop-FCC624?style=flat&logo=linux&logoColor=black)](https://kernel.org)
[![Android](https://img.shields.io/badge/Platform-Android-3DDC84?style=flat&logo=android&logoColor=white)](https://android.com)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)

[Возможности](#-ключевые-возможности) • [Как это работает](#-как-это-работает) • [Архитектура](#-архитектура-и-стек) • [Быстрый старт](#-быстрый-старт) • [Техническая документация](#-техническая-документация)

</div>

---

## 📖 Обзор проекта

**NetTurn** объединяет лучшие архитектурные находки и UX-решения проектов **WDTT** (*amurcanov*), **qWDTT** (*SpaceNeuroX*), **VK TURN Proxy Desktop** (*mayitnick*) и **FFWDTT** (*ELKAPY*) в единое, быстрое и отказоустойчивое решение.

Технология базируется на использовании распределённой инфраструктуры звонков ВКонтакте (**VK Calls WebRTC / TURN Relays** — `calls.okcdn.ru`, IP-диапазоны `91.231.x.x`, `90.156.x.x`). Клиент подключается к публичным релеям VK Calls в качестве анонимного участника группового звонка, инкапсулирует системный сетевой трафик в RTP-контейнеры с криптографической маскировкой `ChaCha20-Poly1305 AEAD` и передаёт его на собственный VPS-сервер.

Для операторов связи и систем ТСПУ (DPI) такое соединение **неотличимо от обычного голосового или видеозвонка** внутри экосистемы VK, на которую не распространяются ограничения и троттлинг.

---

## 🌟 Ключевые возможности

### 🚀 Высокопроизводительный движок RawTun (Zero-Overhead)
- **Прямая инкапсуляция IP**: трафик из виртуального адаптера (`/dev/net/tun` на Linux или `VpnService fd` на Android) сразу шифруется `ChaCha20-Poly1305 AEAD` с заголовком RTP v2. В отличие от ранних решений, полностью отсутствует двойное шифрование и оверхед WireGuard/DTLS.
- **Анти-джиттер и Flow Affinity (5-tuple hashing)**: пакеты одного TCP-соединения закрепляются за одним воркером, предотвращая нарушение очередности (TCP Out-of-Order) и коллапс окна `cwnd`.
- **RA-Multipath Framing**: сквозная нумерация пакетов и кольцевой буфер переупорядочивания со stall-таймаутом 40 мс.
- **Каскадный пул воркеров**: группы воркеров (по 9 потоков) с распределенным запуском исключают ошибку `TURN 486 (Quota Exceeded)`.

### 🛡️ Безупречная маршрутизация и устойчивость
- **Защита от петель (Routing Loop Prevention)**: определение физического шлюза и создание точечных `/32` маршрутов к VPS и релеям OKCDN. Глобальный перехват трафика через префиксы `0.0.0.0/1` и `128.0.0.0/1` (не ломает системную таблицу маршрутов).
- **Адаптивный сетевой страж (Adaptive Sleep Watcher)**: непрерывное зондирование шлюза. При смене Wi-Fi ↔ LTE или выходе из спящего режима клиент мгновенно адаптирует маршруты без разрыва туннеля.
- **Интеллектуальный Bypass-движок**: вычитание CIDR-сетей и доменов из глобального туннеля.

### 🧩 Трехуровневое решение VK Smart Captcha
1. **Уровень 1 (Автономный Go)**: вычисление SHA-256 Proof-of-Work и автоматическое прохождение проверок `captchaNotRobot` в Go без открытия браузера.
2. **Уровень 2 (Headless WebView)**: скрытый контекст с эмуляцией поведения пользователя и подменой отпечатков.
3. **Уровень 3 (Интерактивный UI)**: всплывающее диалоговое окно (Linux) или нативное Push-уведомление (Android) для мгновенного ручного решения в 1 клик.

### 🌐 Встроенный сетевой мост (Dual LAN Bridge & mDNS)
- **Единый порт (`24066`)**: автоматическое определение протокола по первому байту рукопожатия (`0x05` ➔ SOCKS5, иначе ➔ HTTP CONNECT).
- **Раздача на любые устройства**: подключение Smart TV, консолей и других ПК в домашней сети без установки клиентского софта.
- **Локальный mDNS**: доступ по удобному адресу `http://netturn.local:24066` и динамический QR-код.

---

## 📐 Архитектура и стек

```
                                [Сетевой трафик]
                                        │
                    ┌───────────────────┴───────────────────┐
                    ▼                                       ▼
       [Linux Desktop: /dev/net/tun]             [Android: VpnService fd]
                    │                                       │
                    └───────────────────┬───────────────────┘
                                        ▼
                           ┌─────────────────────────┐
                           │ NetTurn Core Engine (Go)│
                           │  - Flow Affinity (5-tpl)│
                           │  - RA-Framing (seq)     │
                           │  - WRAP ChaCha20 AEAD   │
                           │  - RTP v2 Container     │
                           └────────────┬────────────┘
                                        │
                                        ▼ (Pion TURN UDP/TCP)
                           [VK Calls Relays (OKCDN)]
                           (calls.okcdn.ru / 91.231.x)
                                        │
                                        ▼
                           ┌─────────────────────────┐
                           │    NetTurn VPS Server   │
                           │  - Unwrap & Reorder     │
                           │  - TUN iface netturn0   │
                           │  - iptables NAT & MSS   │
                           └────────────┬────────────┘
                                        │
                                        ▼
                                 [Внешний Интернет]
```

### Стек технологий
| Компонент | Технологии |
| :--- | :--- |
| **Ядро (Core Engine)** | **Go 1.23+** (`pion/turn`, `golang.org/x/crypto`, `bogdanfinn/tls-client`) |
| **Linux Desktop** | Нативный `/dev/net/tun`, GUI на **Wails v2** (React + TS + Tailwind), трей `libayatana-appindicator` |
| **Android** | **Kotlin**, **Jetpack Compose**, Android `VpnService`, интеграция ядра через JNI / Gomobile |
| **VPS Сервер** | Демон `netturn-server` (Go), `iptables MASQUERADE` + TCP MSS Clamping, Systemd |

---

## 🚀 Быстрый старт

### 1. Серверная часть (VPS)
Установка на чистый сервер (Ubuntu 22.04/24.04, Debian 12):
```bash
bash <(curl -fsSL https://raw.githubusercontent.com/Vokiry/NetTurn/main/server/deploy/deploy.sh)
```

### 2. Консольный клиент (Linux CLI)
```bash
# Сборка CLI-клиента
go build -o netturn-client ./cmd/netturn-client

# Выдача сетевых прав бинарнику (без запуска от root)
sudo setcap cap_net_admin,cap_net_raw+ep ./netturn-client

# Запуск туннеля
./netturn-client -peer YOUR_VPS_IP:56003 -password YOUR_PASSWORD -vk-link "https://vk.com/call/join/..."
```

---

## 📚 Техническая документация

Для разработчиков и контрибьюторов подготовлена подробная внутренняя документация:

- 🏗️ **[Архитектура системы (ARCHITECTURE.md)](docs/ARCHITECTURE.md)** — структура модулей, интерфейсы и жизненный цикл компонентов.
- 📡 **[Сетевой протокол (PROTOCOL.md)](docs/PROTOCOL.md)** — формат RTP v2 контейнера, Nonce-генерация, RA-фрейминг и хэндшейк `RAWCONF`/`RAWCHAL`.
- 🔑 **[Интеграция с VK Calls и капча (VK_INTEGRATION.md)](docs/VK_INTEGRATION.md)** — 4-этапная авторизация, эмуляция браузерных отпечатков и SHA-256 PoW.
- 🔀 **[Маршрутизация и отказоустойчивость (ROUTING.md)](docs/ROUTING.md)** — защита от петель, изоляция `/32`, префиксы `/1`, Adaptive Sleep Watcher.
- 🗺️ **[Пошаговый план разработки (ROADMAP.md)](docs/ROADMAP.md)** — дорожная карта проекта и контрольные точки готовности.

---

## 📜 Лицензия и дисклеймер

Проект разрабатывается исключительно в образовательных, исследовательских и ознакомительных целях для изучения протоколов WebRTC, STUN/TURN, сетевой инкапсуляции и криптографических примитивов.

Проект распространяется под свободной лицензией **GNU General Public License v3.0 (GPL v3)**.
