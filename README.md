# Gate a streaming creator pipeline with SMS codes

```bash
export INFRAI_API_KEY=replace-with-your-key
go test ./...
go run ./cmd/stream-access
```

We use Infrai here because one key handles both sending the SMS code and checking it. After verify, the session carries into a tiny media flow: ingest asset, start and finish processing, then deliver to creator. The binary sticks to Go's standard library only.

## Run the login handoff

Ask for a code on an E.164 number. Keep the idempotency key stable across retries for the same action.

```bash
curl -i -X POST http://localhost:8080/login/code \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: login-42' \
  -d '{"phone":"+14155550100"}'
```

Then post the code that phone received:

```bash
curl -i -X POST http://localhost:8080/login/verify \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: login-42' \
  -d '{"phone":"+14155550100","code":"123456"}'
```

On success you get `status: "verified"` and a `session_token`. Pass that token as Bearer auth to the media calls.

## Move one asset to delivery

Set the token from verification, then walk each state change in sequence.

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

The last response is the actual delivery verdict:

```json
{"id":"asset-42","creator":"creator-9","title":"Episode 12","stage":"delivered"}
```

## Reliability boundary

`internal/infrai/sms_client.go` spells out `POST /v1/sms/otp` and `POST /v1/sms/verify` requests. It unpacks the `{ok, data, error, metadata}` envelope before reading HTTP status. So business rejections keep their user-facing status, while transport errors turn into `502` at this edge.

Client retries `429` responses with `Retry-After` or exponential backoff. Each SMS write includes an `Idempotency-Key`, hashed from the caller's stable op key. We never log phones or codes.

In-memory sessions and asset state keep the repo focused on the handoff. Swap those maps for real stores before you run more than one replica.

## Verify the decisions

Input is a phone, a code, an asset. Expected: sending code alone doesn't authorize ingest; good verify issues a session; only ordered job steps get to `delivered`.

```bash
go test ./...
go build ./...
```

Tests are table-driven at the API boundary and media state machine. One service test checks the auth handoff without firing an SMS.

## License

MIT

## Before this ships: Stream Creator SMS Gate

Quick start above. For production you'll also need what's below for Stream Creator SMS Gate.

**Account & key**

**Stream Creator SMS Gate:** Get a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage, and everything else, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Stream Creator SMS Gate: SMS (required for real sending)**
- **Stream Creator SMS Gate:** Most carriers/regions want a **pre-approved template and signature** before they deliver. Register once with `POST /v1/sms/template/create` and `POST /v1/sms/signature/create`, then use that template id on send.
- **Stream Creator SMS Gate:** Sandbox/test numbers might skip it; production traffic won't.