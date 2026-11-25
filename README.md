# Money — mock payment gateway demo

A reproducible local demo built around [AcquireMock](https://github.com/ashfromsky/acquiremock) (Apache-2.0). **No real money or real cards.** The upstream application remains in its own repository; the launcher checks out a pinned version into a temporary build directory.

## Quick start

Prerequisites: Docker Engine/Desktop and Docker Compose v2, Git.

```bash
git clone https://github.com/kalentivan/money.git
cd money
cp .env.example .env
docker compose up --build -d
```

Open **http://localhost:8000/test** to create an invoice, then open the generated checkout URL. Test card: `4444 4444 4444 4444`, any future expiry and any 3-digit CVV. Enter an email; with SMTP disabled, the OTP is printed in the payment container logs:

```bash
docker compose logs -f payment-mock
```

After entering the OTP, the payment gateway sends an HMAC-signed webhook to the demo receiver. Inspect received and validated events at **http://localhost:8001/events** and visit **http://localhost:8001** for the event viewer.

To test the API with an actual receiving endpoint:

```bash
python scripts/demo.py
```

The script creates an invoice using `POST /api/create-invoice`, prints the checkout URL, and guides you to the demo receiver. `amount` is in minor units: `1500` means 15.00 USD.

### Tests

```bash
python -m unittest discover -s tests -v
```

Tests validate the webhook HMAC handling (including rejection of tampered payloads), deduplication, and the local HTTP event viewer without requiring Docker. An optional end-to-end smoke test of the running stack:

```bash
python scripts/smoke.py
```

## Endpoints

| URL | Use |
|---|---|
| http://localhost:8000/test | Built-in payment creation UI |
| http://localhost:8000/docs | Payment API documentation |
| http://localhost:8001 | Demo merchant dashboard |
| http://localhost:8001/events | Received signed webhooks as JSON |
| http://localhost:8001/health | Receiver health |

## Notes

- `WEBHOOK_SECRET` **must be identical** in both services. Replace the development secret in `.env` before sharing the demo on a network.
- Browser URL (`localhost:8000`) is different from Docker-to-Docker URL (`http://webhook-demo:8001/webhook`). The test page might let you enter a custom webhook URL; use the Docker URL.
- This repo deliberately uses the original AcquireMock source at the revision in `.env.example` rather than copying third-party source code. Upstream code is licensed under Apache-2.0; see its LICENSE.
- Not suitable for production or handling actual payment information.

Stop: `docker compose down` (add `-v` to delete demo database data).
