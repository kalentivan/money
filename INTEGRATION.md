# Интеграция платёжного мока AcquireMock

## Запуск

```bash
docker compose up -d --build
```

- http://localhost:8080 — демонстрационный кинотеатр.
- http://localhost:8000/docs — Swagger платёжного API.
- Внутри сети Docker адрес API: http://payment-mock:8000.

## 1. Создание платежа

Backend кинотеатра проверяет владельца бронирования и рассчитывает стоимость на сервере.

```http
POST /api/create-invoice
Content-Type: application/json
```

```json
{
  "amount": 1500,
  "reference": "BOOKING-1001",
  "webhookUrl": "http://cinema-backend:8080/webhook",
  "redirectUrl": "http://localhost:8080/"
}
```

| Поле | Назначение |
|---|---|
| amount | Положительная целая сумма в рублях в текущей конфигурации (1500 = 1500 ₽) |
| reference | Идентификатор бронирования в системе кинотеатра |
| webhookUrl | Доступный из контейнера платёжного сервиса адрес backend для уведомлений |
| redirectUrl | Адрес возврата покупателя после оплаты |

Ответ HTTP 200:

```json
{"pageUrl":"http://localhost:8000/checkout/<payment_uuid>"}
```

Сохраните идентификатор платежа из pageUrl вместе с бронированием. Перенаправьте браузер на pageUrl без дополнительных параметров. Не передавайте карточные данные через backend кинотеатра.

## 2. Тестовая оплата

Карта: 4444 4444 4444 4444; срок: 12/29; CVV: 123; email: любой тестовый адрес; OTP: 1234. Реальных списаний нет.

## 3. Webhook

После успешной оплаты сервис отправляет POST на webhookUrl с заголовками Content-Type: application/json, X-Signature (hex HMAC-SHA256), X-Payment-ID.

Пример тела:

```json
{
  "payment_id": "97d7e353-675c-48bf-a192-9b6d0a9acff9",
  "reference": "BOOKING-1001",
  "amount": 1500.0,
  "status": "paid",
  "timestamp": "2026-11-25T21:51:58",
  "card_mask": "**** 4444"
}
```

Подпись вычисляется над JSON в формате Python json.dumps(payload, sort_keys=True), включая стандартные пробелы и ASCII-экранирование. Секрет WEBHOOK_SECRET должен совпадать на обеих сторонах. Пример проверки на Python:

```python
import hashlib
import hmac
import json
import os

def verify(payload, signature):
    message = json.dumps(payload, sort_keys=True).encode()
    expected = hmac.new(
        os.environ["WEBHOOK_SECRET"].encode(), message, hashlib.sha256
    ).hexdigest()
    return hmac.compare_digest(expected, signature)
```

После проверки подписи сверить payment_id, reference, сумму и статус с сохранённым платежом. Только после этого подтвердить бронирование. Повторные уведомления обрабатывать идемпотентно. Ответить HTTP 200 после успешной обработки. Возврат браузера сам по себе не подтверждает оплату.

## 4. Ошибки и ограничения

- 402 INSUFFICIENT_FUNDS — неверная тестовая карта или отказ.
- 409 PAYMENT_ALREADY_PROCESSED — повторная попытка оплаты завершённого платежа.
- После отказа создать новый платёж; текущий мок не отправляет webhook отказа в этом сценарии.
- Счёт действует 15 минут.
- Это локальный тестовый сервис: API создания платежа не защищён API-ключом, OTP фиксирован, данные демо-магазина хранятся в памяти. Не публиковать в интернете и не использовать реальные карты. Для реального приложения необходимы собственная авторизация, постоянное хранение платежей и защищённый доступ к API.

## 5. Демонстрация

Открыть http://localhost:8080, выбрать Алису, ввести бронь 1001, нажать «Оплатить», пройти оплату и вернуться в кинотеатр. После webhook бронь должна отображаться как оплаченная. Заказ 2001 принадлежит Борису и недоступен Алисе.
