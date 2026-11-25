# Money — Go payment mock demo

A local demo of a mock payment provider using [AcquireMock](https://github.com/ashfromsky/acquiremock) (Apache-2.0), a **Go merchant UI**, and PostgreSQL. No real money or real card numbers.

## Run with Docker

Requires Docker Engine/Desktop with Compose v2. The payment service is built from a pinned upstream Git commit.

```bash
git clone https://github.com/kalentivan/money.git
cd money
cp .env.example .env
docker compose up -d --build
```

Visit **http://localhost:8080** — this is the **Go UI** for the demo merchant.

1. Enter a reference and amount (in cents; `1500` means $15).
2. Click "Создать счёт"; click the resulting "Открыть оплату" button.
3. Enter **test card** `4444 4444 4444 4444`, any future expiry, any 3-digit CVV, and a test email.
4. With SMTP disabled, retrieve OTP in the gateway container logs: `docker compose logs -f payment-mock`.
5. Enter the OTP; a signed webhook is sent to Go and appears in the Go UI automatically (refreshes every 3 seconds).

Additional pages: **http://localhost:8000/test** (upstream UI), **http://localhost:8000/docs** (gateway Swagger), **http://localhost:8080/events** (merchant webhook JSON), **http://localhost:8080/health**.

## Test

Go 1.23 or newer:

```bash
cd go-demo
go test -race -v ./...
```

CI automatically runs Go tests and builds the app on every push.

## Architecture

```text
Browser -> Go UI (:8080) -> POST /api/create-invoice -> AcquireMock (:8000)
Browser -> AcquireMock checkout + OTP -> POST /webhook -> Go UI
Go UI -> HMAC-SHA256 verification -> in-memory event list -> Browser
AcquireMock -> PostgreSQL (:5432, internal Docker network)
```

The Go demo implements the merchant-side integration and webhook receiver. The original AcquireMock gateway stays upstream and is built at a pinned revision in Compose. Webhook event history in Go is **in-memory** and resets with its container; gateway payments persist in the PostgreSQL volume.

The gateway signs the sorted JSON webhook payload using HMAC-SHA256. Go verifies the signature using a shared secret and deduplicates events by payment ID + status. Only signed events appear in the UI.

**Configuration:** Copy `.env.example` to `.env`. Set `WEBHOOK_SECRET` to a strong secret before exposing this to another host. Both containers must use the same secret. `BASE_URL` is the browser-accessible URL to the gateway (by default `http://localhost:8000`). For local demonstrations only; not production-ready, and never use actual payment card data.

Stop and clean up:

```bash
docker compose down
docker compose down -v # also removes the demo database
```
