# Интеграция с VK Calls и обход защиты (Внутренняя документация)

> **Статус**: Архитектурная спецификация  
> **Инфраструктура**: OKCDN (`calls.okcdn.ru`), VK API (`api.vk.ru`, `login.vk.ru`)  

Документ описывает процедуру взаимодействия с закрытым API групповых звонков ВКонтакте, алгоритмы автоматического решения капчи (PoW) и защиту от детекта со стороны систем антифрода.

---

## 1. Пайплайн авторизации VK Calls

Для получения параметров доступа к медиа-релеям (TURN) клиент проходит многоэтапную цепочку обмена токенами:

```
[NetTurn Core]
       │
       │ 1. POST https://login.vk.ru/?act=get_anonym_token
       ▼
 [Токен 1: access_token]
       │
       │ 2. POST https://api.vk.ru/method/calls.getCallPreview
       ▼
 [Валидация звонка]
       │
       │ 3. POST https://api.vk.ru/method/calls.getAnonymousToken
       │    (ТРИГГЕР КАПЧИ: VK Smart Captcha error_code 14)
       ▼
 [Токен 2: anonymToken]
       │
       │ 4. POST https://calls.okcdn.ru/fb.do (method=auth.anonymLogin)
       ▼
 [Токен 3: session_key]
       │
       │ 5. POST https://calls.okcdn.ru/fb.do (method=vchat.joinConversationByLink)
       ▼
[TURN Credentials] ➔ IP-адреса релеев, username, password
```

### 1.1. Шаг 1: Анонимный токен сообщений
- **URL**: `https://login.vk.ru/?act=get_anonym_token`
- **Параметры**:
  - `client_id`, `client_secret` — используются валидные идентификаторы официальных приложений VK.
  - `token_type`: `messages`
  - `version`: `1`
- **Ответ**: `data.access_token` (Токен 1).

### 1.2. Шаг 2: Валидация звонка
- **URL**: `https://api.vk.ru/method/calls.getCallPreview?v=5.275`
- **Параметры**:
  - `vk_join_link`: `https://vk.com/call/join/<hash>`
  - `access_token`: Токен 1

### 1.3. Шаг 3: Токен участника звонка
- **URL**: `https://api.vk.ru/method/calls.getAnonymousToken?v=5.275`
- **Параметры**:
  - `vk_join_link`: `https://vk.com/call/join/<hash>`
  - `name`: рандомизированное имя пользователя (UTF-8, URL-encoded)
  - `access_token`: Токен 1
- **Ответ**: `response.token` (Токен 2).
- **Ошибка 14 (Капча)**: если запрос перехвачен WAF, возвращается объект с `captcha_sid`, `redirect_uri` и `session_token`. Запускается движок капчи.

### 1.4. Шаги 4–5: Вход в медиа-сессию OKCDN
- **URL**: `https://calls.okcdn.ru/fb.do`
- **Метод 1 (`auth.anonymLogin`)**:
  - `session_data`: JSON `{ "version": 2, "device_id": "<uuid>", "client_version": 1.1, "client_type": "SDK_JS" }`
  - `application_key`: `CGMMEJLGDIHBABABA`
  - Результат: `session_key` (Токен 3).
- **Метод 2 (`vchat.joinConversationByLink`)**:
  - `joinLink`: ссылка на звонок
  - `anonymToken`: Токен 2
  - `session_key`: Токен 3
  - Результат: объект `turn_server`:
    ```json
    {
      "turn_server": {
        "urls": ["turn:91.231.x.x:3478", "turn:90.156.x.x:3478"],
        "username": "...",
        "credential": "..."
      }
    }
    ```

---

## 2. Трехуровневая система решения VK Smart Captcha

```
             [VK API Ошибка 14: Капча]
                         │
                         ▼
        ┌─────────────────────────────────┐
        │     Уровень 1: Автономный Go    │
        │   - Загрузка bootstrap-страницы │
        │   - Вычисление SHA-256 PoW      │
        │   - Спуфинг поведения курсора   │
        └────────────────┬────────────────┘
                         │
               [Успех?] ─┴─ [Слайдер / Фрод]
                │                     │
                ▼                     ▼
          [Токен выдан]   ┌─────────────────────────────┐
                          │   Уровень 2: Headless WV    │
                          │   - Скрытый браузерный контекст│
                          │   - Эмуляция кликов и свайпа │
                          └──────────────┬──────────────┘
                                         │
                               [Успех?] ─┴─ [Интерактив]
                                │                  │
                                ▼                  ▼
                          [Токен выдан]  ┌─────────────────────┐
                                         │ Уровень 3: UI / Push│
                                         │ - Диалоговое окно   │
                                         │ - Нативный Android  │
                                         └─────────────────────┘
```

### 2.1. Уровень 1: Чистый Go (Proof-of-Work)
1. Клиент загружает страницу `redirect_uri` и извлекает из HTML скрипта:
   - `pow_input`: случайная строка.
   - `difficulty`: целевое количество ведущих нулей (обычно от 3 до 5).
2. **Вычисление**:
   ```go
   target := strings.Repeat("0", difficulty)
   for nonce := 1; nonce <= 10000000; nonce++ {
       hash := sha256.Sum256([]byte(pow_input + strconv.Itoa(nonce)))
       hexHash := hex.EncodeToString(hash[:])
       if strings.HasPrefix(hexHash, target) {
           return hexHash
       }
   }
   ```
3. **Пайплайн запросов к `https://api.vk.ru/method/`**:
   - `captchaNotRobot.settings`: инициализация параметров.
   - `captchaNotRobot.componentDone`: передача отпечатка браузера (`browser_fp`, `device`).
   - `captchaNotRobot.check`: передача вычисленного `hash`, поддельного трека курсора (`generateFakeCursor()`), сетевых таймингов RTT и downlink.
   - `captchaNotRobot.endSession`: финализация сессии.
4. При успешном ответе извлекается `success_token`, который передается обратно в `calls.getAnonymousToken`.

### 2.2. Защита от детекта бота (Anti-Fingerprinting)
- **TLS Client Profile**: Go-клиент использует `tls-client` с профилем `Chrome_130+`, обеспечивая корректный JA3/JA4 отпечаток и порядок TLS расширений.
- **Троттлинг запросов**: обязательный рандомизированный интервал между запросами к VK API (`3000ms + rand(3000ms)`).
- **Global Captcha Lockout**: при исчерпании лимитов включается пауза на 60 секунд для предотвращения временного бана IP-адреса.
- **Кеширование креденшелов**: один набор TURN-креденшелов кешируется на группу из 10 воркеров на 9 минут (при TTL токена 10 минут).
