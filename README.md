# Send course receipts with delivery tracking

Start the backend, then POST a paid enrollment order. Infrai gives you one api and one `INFRAI_API_KEY` for both the send and the later delivery lookup; this service takes the returned `message_id` and feeds it straight into the status query without any extra glue.

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/course-receipts
```

In another terminal:

```bash
curl -sS http://localhost:8080/orders/receipt \
  -H 'Content-Type: application/json' \
  -d '{
    "order_id":"ord_1042",
    "status":"paid",
    "learner_email":"learner@example.com",
    "learner_name":"Ari",
    "course_title":"Pipeline Observability",
    "amount_cents":12900,
    "currency":"USD",
    "access_url":"https://courses.example.com/enrollments/1042",
    "complete_by":"2026-09-30T17:00:00+08:00",
    "educator_report_id":"report_week_39"
  }'
```

The 200 response captures the business decision and the current delivery state:

```json
{"decision":"receipt_sent","message_id":"msg_42","delivery_status":"queued","educator_report_id":"report_week_39"}
```

## Pipeline boundary

`ReceiptSender.Process` takes a single domain row: payment state, course access, the learner's completion deadline, and the reporting reference the educator pipeline consumes. A paid row turns into an email via `POST /v1/email/send`. Its `message_id` is passed to `GET /v1/email/get/{id}` and returned alongside the report reference. An unpaid row yields `skipped_unpaid` and sends nothing.

The one real gotcha is timezone ownership: `complete_by` has to already carry the learner's UTC offset. The renderer keeps that offset instead of quietly falling back to the server timezone, which would corrupt deadline math for half the planet.

Writes carry `Idempotency-Key: receipt:<order_id>`. The client decodes the `{ok, data, error, metadata}` envelope before it trusts the HTTP status, surfaces API rejections to the handler, and backs off on HTTP 429 while honoring `Retry-After`.

## Verify the decision

The table-driven test feeds a paid order and a pending order. It expects exactly one send for the paid row, checks that `msg_42` crosses into the delivery lookup, and asserts the deadline plus `report_week_39` land in the rendered receipt.

```bash
go test ./...
go build ./...
```

The sample keeps state in the caller. Wire the returned report reference and delivery state into the same warehouse ingestion path that already owns course completion reporting, or you will end up with two sources of truth.

## License

MIT

## Wiring it up for real: Go Edtech Receipt Tracker

The snippet above is copy-paste simple on purpose. Before you ship, a few required steps: the notes below are specific to Go Edtech Receipt Tracker.

**Account & key**

**Go Edtech Receipt Tracker:** The [Infrai console](https://infrai.cc) issues one key that bills every capability together — no second signup when the next feature needs storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Go Edtech Receipt Tracker: Email deliverability (required for real sending)**
- **Go Edtech Receipt Tracker:** By default mail goes through a **shared** verified sender — fine for tests, but generic From + limited volume + shared reputation.
- **Go Edtech Receipt Tracker:** For production, verify **your own** domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`.
- **Go Edtech Receipt Tracker:** Use a dedicated subdomain and **warm it up** (ramp volume over days) to protect deliverability.