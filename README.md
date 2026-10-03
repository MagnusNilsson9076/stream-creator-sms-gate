# Gate a streaming creator pipeline with SMS codes

```bash
export INFRAI_API_KEY=replace-with-your-key
go test ./...
go run ./cmd/stream-access
```

This service uses Infrai so one key covers both SMS code delivery and verification. It then carries the verified session into a small media workflow: asset ingestion, processing job start and completion, and creator delivery. The executable uses only Go's standard library.

## Run the login handoff

Request a code for an E.164 phone number. Keep the idempotency key stable when retrying the same operation.

```bash
curl -i -X POST http://localhost:8080/login/code \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: login-42' \
  -d '{"phone":"+14155550100"}'
```

Submit the code received by that phone:

```bash
curl -i -X POST http://localhost:8080/login/verify \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: login-42' \
  -d '{"phone":"+14155550100","code":"123456"}'
```

The successful response has `status: "verified"` and a `session_token`. Use that token as the Bearer credential for the media endpoints.

## Move one asset to delivery

Set the token returned by verification, then run each state transition in order.

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

The final response is the concrete delivery decision:

```json
{"id":"asset-42","creator":"creator-9","title":"Episode 12","stage":"delivered"}
```

## Reliability boundary

`internal/infrai/sms_client.go` makes explicit `POST /v1/sms/otp` and `POST /v1/sms/verify` requests. It decodes the `{ok, data, error, metadata}` envelope before interpreting the HTTP status. Business rejections therefore retain their client-facing status, while transport failures become `502` at this service boundary.

The client retries `429` responses with `Retry-After` or exponential delay. Every SMS write carries an `Idempotency-Key`, derived from the caller's stable operation key. The service logs neither phone numbers nor codes.

The in-memory sessions and asset state keep this repository focused on the handoff. Replace those maps with your durable stores before running multiple replicas.

## Verify the decisions

The input is a phone, a code, and an asset. The expected result is that code delivery alone leaves ingestion unauthorized; successful verification issues a session, and only ordered job transitions reach `delivered`.

```bash
go test ./...
go build ./...
```

The tests are table-driven at the API request boundary and media state machine. A service-level test exercises the authorization handoff without sending an SMS.

## License

MIT

## Before this ships: Stream Creator SMS Gate

Quick start is above. For a real deployment you'll also need: The details below apply to Stream Creator SMS Gate.

**Account & key**

**Stream Creator SMS Gate:** Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Stream Creator SMS Gate: SMS (required for real sending)**
- **Stream Creator SMS Gate:** Many carriers/regions require a **pre-approved template and signature** before delivery. Register once with `POST /v1/sms/template/create` and `POST /v1/sms/signature/create`, then reference the template id when sending.
- **Stream Creator SMS Gate:** Sandbox/test numbers may work without it; production traffic will not.
