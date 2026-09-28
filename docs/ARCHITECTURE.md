# Архитектура NetTurn (Внутренняя документация)

> **Статус**: Архитектурная спецификация  
> **Целевые платформы**: Linux Desktop, Android  
> **Базовый язык ядра**: Go 1.23+  

Документ описывает внутреннюю компонентную структуру, абстракции и жизненный цикл подсистем проекта **NetTurn**.

---

## 1. Концептуальная модель

NetTurn — высокопроизводительный туннель, маскирующий системный IP-трафик под медиа-потоки звонков ВКонтакте (**VK Calls WebRTC / TURN**).

```
[Пользовательский трафик: ОС / Приложения]
                   │
                   ▼
┌─────────────────────────────────────────────────────────────┐
│                  NetTurn Client Core (Go)                   │
│                                                             │
│   ┌───────────────────────────┐ ┌───────────────────────┐   │
│   │  Linux TUN (/dev/net/tun) │ │  Android VpnService   │   │
│   │    (Native Linux Client)  │ │      (Parcel FD)      │   │
│   └─────────────┬─────────────┘ └───────────┬───────────┘   │
│                 └─────────────┬─────────────┘               │
│                               ▼                             │
│                    [Flow Dwell Dispatcher]                  │
│                     - 5-tuple flow affinity                 │
│                     - Anti-jitter буферизация               │
│                     - RA-multipath framing                  │
│                               │                             │
│                               ▼                             │
│                     [WRAP AEAD Encryptor]                   │
│                     - HKDF-SHA256 деривация                 │
│                     - ChaCha20-Poly1305                     │
│                     - RTP v2 контейнеризация                │
│                               │                             │
│                               ▼                             │
│                   [Pion TURN Client Pool]                   │
│                     - Пул воркеров (кратный 9)              │
│                     - Staggered запуск (100-300ms)          │
└───────────────────────────────┬─────────────────────────────┘
                                │
                                ▼ (TURN UDP / TCP Data Channels)
                  [VK Calls TURN Media Relays]
             (calls.okcdn.ru / 91.231.x.x, 90.156.x.x)
                                │
                                ▼
┌─────────────────────────────────────────────────────────────┐
│                     NetTurn VPS Server                      │
│                                                             │
│   [WRAP Listener] ──► [Unwrap & Reorder] ──► [TUN netturn0] │
│                                                     │       │
│                                                     ▼       │
│                                             [iptables NAT]  │
│                                                     │       │
│                                                     ▼       │
│                                             [Внешний Сеть]  │
└─────────────────────────────────────────────────────────────┘
```

---

## 2. Структура монорепозитория

```
NetTurn/
├── core/                           # Кроссплатформенное ядро (Go 1.23+)
│   ├── engine.go                   # Единая точка входа API: Start(), Stop(), Stats()
│   ├── types.go                    # Конфигурации (EngineConfig), структуры статусов
│   ├── eventbus/                   # Реактивная шина событий для UI
│   ├── crypto/                     # Криптография WRAP: HKDF + ChaCha20-Poly1305
│   ├── protocol/                   # Протокол пакетов: RTP v2, RA Frame, Flow Affinity
│   ├── rawtun/                     # Диспетчер трафика и абстракция TUN интерфейса
│   │   ├── tun.go                  # Интерфейс TunDevice
│   │   ├── tun_linux.go            # Linux TUN (/dev/net/tun)
│   │   ├── tun_android.go          # Android VpnService wrapper (fd -> os.File)
│   │   └── dispatcher.go           # Flow Dwell диспетчер и балансировка
│   ├── turn/                       # Pion TURN клиент и управление пулом воркеров
│   ├── vk/                         # Интеграция с VK Calls API
│   │   ├── client.go               # Браузерный HTTP клиент (tls-client)
│   │   ├── auth.go                 # 4-шаговая авторизация и получение креденшелов
│   │   ├── pool.go                 # Пул хешей звонков и их ротация
│   │   └── captcha/                # 3-уровневый движок решения капчи
│   ├── network/                    # Сетевой мониторинг и маршрутизация
│   │   ├── route_linux.go          # Маршрутизация Linux (/32 исключения, 0.0.0.0/1)
│   │   ├── watcher.go              # Adaptive Sleep Watcher (TCP-проба шлюза)
│   │   └── bypass.go               # Алгоритм вычитания сетей (CIDR subtraction)
│   └── bridge/                     # Локальный сетевой мост (Dual Proxy: SOCKS5 + HTTP)
│
├── cmd/
│   ├── netturn-client/             # Консольный клиент (CLI) для Linux Desktop/серверов
│   └── netturn-server/             # Серверный демон для VPS
│
├── desktop/                        # Wails v2 GUI для Linux Desktop
│   ├── main.go                     # Точка входа Wails
│   ├── app.go                      # Биндинги Go-ядра к интерфейсу
│   ├── tray.go                     # Системный трей (libayatana-appindicator)
│   └── frontend/                   # UI на React + TypeScript + Tailwind CSS
│
├── android/                        # Мобильное приложение для Android
│   ├── app/src/main/kotlin/        # Jetpack Compose UI + VpnService
│   └── core-bridge/                # Gomobile биндинги к Go-ядру
│
├── server/                         # Серверные скрипты и конфигурации VPS
│   ├── deploy/deploy.sh            # Скрипт развертывания одной командой
│   └── systemd/netturn.service     # Systemd unit файл
│
└── docs/                           # Внутренняя техническая документация
    ├── ARCHITECTURE.md             # Настоящий документ
    ├── PROTOCOL.md                 # Спецификация Wire-протокола
    ├── VK_INTEGRATION.md           # Спецификация VK API и капчи
    ├── ROUTING.md                  # Маршрутизация и отказоустойчивость
    └── ROADMAP.md                  # Пошаговый план разработки
```

---

## 3. Абстракции компонентов

### 3.1. Интерфейс сетевого адаптера (`TunDevice`)
```go
type TunDevice interface {
    io.ReadWriteCloser
    MTU() int
    Name() string
}
```
- **Linux Desktop**: реализуется через открытие `/dev/net/tun` с системным флагом `unix.IFF_TUN | unix.IFF_NO_PI`.
- **Android**: реализуется через `os.NewFile(uintptr(fd), "tun")`, где дескриптор `fd` передан из Kotlin (`VpnService.Builder().establish().detachFd()`).

### 3.2. Жизненный цикл сессии ядра (`Engine`)
1. **Config Validation**: проверка входных параметров (адрес VPS, пароль, хеши звонков, режим работы).
2. **VK Initialization**: запрос TURN креденшелов через пул звонков (`vk/pool.go`). При необходимости — запуск решателя капчи (`vk/captcha/`).
3. **Physical Gateway Probe**: определение текущего шлюза и физического интерфейса через `network/route_linux.go`.
4. **Protective Routing**: установка статических маршрутов `/32` до IP VPS и IP-адресов TURN-серверов.
5. **Worker Pool Spawn**: запуск пула воркеров (по умолчанию 9) с каскадной паузой 100 мс для предотвращения `TURN 486 (Quota Exceeded)`.
6. **Handshake**: первый воркер согласует параметры через протокол `RAWCONF` / `RAWCHAL` и получает IP-адрес клиента (`10.70.x.y`).
7. **TUN Creation & Routing**: активация виртуального интерфейса и добавление маршрутов `0.0.0.0/1` и `128.0.0.0/1`.
8. **Watchdog Activation**: запуск фонового сетевого стража (`watcher.go`).

---

## 4. Потоки данных (Data Flow)

### Uplink (Клиент ➔ Сервер)
1. Пакет читается из `TunDevice` (размер до MTU 1280).
2. Вычисляется 5-tuple хеш `rawFlowKey` (SrcIP, DstIP, SrcPort, DstPort, Proto).
3. По хешу определяется закрепленный за потоком воркер (`flowAffinity`).
4. Пакет оборачивается в заголовок `RA-Frame` (Magic `0x5241` + инкрементный `seq uint32`).
5. Данные шифруются `ChaCha20-Poly1305` и оборачиваются в RTP v2 пакет со случайным паддингом.
6. Отправка через сокет Pion TURN в медиа-релей VK, откуда пакет пересылается на VPS.

### Downlink (Сервер ➔ Клиент)
1. Сервер принимает RTP-кадр из TURN-канала, валидирует SSRC/seq/ts, дешифрует `ChaCha20-Poly1305`.
2. Извлекает IP-пакет, передает его в системный `netturn0`, откуда пакет уходит в интернет через `iptables MASQUERADE`.
3. Обратный трафик из интернета читается сервером из `netturn0`.
4. Сервер определяет сессионную группу по Destination IP (`10.70.x.y`), шифрует и отправляет обратно через закрепленный воркер.
5. Клиент принимает пакет, дешифрует, пропускает через буфер восстановления порядка (`rawReorder`) и пишет в виртуальный адаптер.
