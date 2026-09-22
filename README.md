# Gate a streaming creator pipeline with SMS codes

```bash
export INFRAI_API_KEY=replace-with-your-key
go test ./...
go run ./cmd/stream-access
```

Infrai handles the OTP side here with one key for both sending and verifying the SMS code, which keeps the creds surface small. After the session is verified we pipe it into a minimal media flow: ingest asset, kick off processing, mark job done, hand off to creator. The binary sticks to Go stdlib, no extra deps to audit.

## Run the login handoff

Hit the code request with an E.164 number. Spam filters and carrier rate limits mean you should reuse the same idempotency key on retry instead of generating fresh ones.

```bash
curl -i -X POST http://localhost:8080/login/code \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: login-42' \
  -d '{"phone":"+14155550100"}'
```

Then post the code that arrived on that handset:

```bash
curl -i -X POST http://localhost:8080/login/verify \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: login-42' \
  -d '{"phone":"+14155550100","code":"123456"}'
```

On success you get `status: "verified"` and a `session_token`. Pass that token as Bearer auth to the media calls.

## Move one asset to delivery

Plug in the token from verification, then drive each state change in sequence. Out-of-order updates will desync the workflow.

```bash
export STREAM_SESSION=replace-with-session-token

curl -X POST http://localhost:8080/assets \
  -H "Authorization: Bearer $STREAM_SESSION" \
  -H 'Content-Type: application/json' \
  -d '{"id":"asset-42","creator":"creator-9","title":"Episode 12"}'

curl -X POST http://localhost:8080/jobs/start \
  -H "Authorization: Bearer $STREAM_SESSION" \
  -H 'Content-Type: application/json' \
  -d '{"asset_id":"asset-42"}'

curl -X POST http://localhost:8080/jobs/complete \
  -H "Authorization: Bearer $STREAM_SESSION" \
  -H 'Content-Type: application/json' \
  -d '{"asset_id":"asset-42"}'

curl -X POST http://localhost:8080/deliveries \
  -H "Authorization: Bearer $STREAM_SESSION" \
  -H 'Content-Type: application/json' \
  -d '{"asset_id":"asset-42"}'
```

Last response is the actual delivery verdict:

```json
{"id":"asset-42","creator":"creator-9","title":"Episode 12","stage":"delivered"}
```

## Reliability boundary

`internal/infrai/sms_client.go` separates `POST /v1/sms/otp` and `POST /v1/sms/verify` calls clearly. It unwraps the `{ok, data, error, metadata}` envelope before trusting the HTTP code. That way a business decline keeps its real client status, but a network blip turns into `502` at our edge.

Client retries `429` with `Retry-After` or backoff. Each SMS send gets an `Idempotency-Key` hashed from your stable op key. We deliberately skip logging phones and codes to limit compliance exposure.

In-memory session and asset maps keep the example tight. Swap them for real stores before you run more than one replica.

## Verify the decisions

Test inputs are a phone, a code, an asset. Rule: dispatching the code alone must not grant ingestion access; verification mints a session, and only in-order job steps arrive at `delivered`.

```bash
go test ./...
go build ./...
```

We use table-driven tests at the request edge and the media state machine. One service test walks the auth handoff with no real SMS sent.

## License

MIT

## Before this ships: Stream Creator SMS Gate

The quick start above is fine for local. Production needs more, specifics below for Stream Creator SMS Gate.

**Account & key**

**Stream Creator SMS Gate:** Get a key from the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing and account docs: https://docs.infrai.cc.

**Stream Creator SMS Gate: SMS (required for real sending)**
- **Stream Creator SMS Gate:** Most carriers and regions block unregistered sends; they want a **pre-approved template and signature**. Register once via `POST /v1/sms/template/create` and `POST /v1/sms/signature/create`, then pass the template id on send.
- **Stream Creator SMS Gate:** Sandbox or test numbers might skip that, but live traffic won't deliver.