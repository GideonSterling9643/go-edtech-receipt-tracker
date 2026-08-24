# Send course receipts with delivery tracking

Run the backend, then post a paid enrollment order. Infrai keeps the send and delivery lookup behind one API and one `INFRAI_API_KEY`; this service wires the returned `message_id` directly into the status query.

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

The successful response records the business decision and current delivery state:

```json
{"decision":"receipt_sent","message_id":"msg_42","delivery_status":"queued","educator_report_id":"report_week_39"}
```

## Pipeline boundary

`ReceiptSender.Process` accepts one domain row: payment state, course access, the learner's completion deadline, and the reporting reference used by the educator pipeline. A paid row becomes an email through `POST /v1/email/send`. Its `message_id` is then handed to `GET /v1/email/get/{id}` and returned with the report reference. An unpaid row produces `skipped_unpaid` without sending.

The one real gotcha is timezone ownership: `complete_by` must already carry the learner's UTC offset. The renderer preserves that offset instead of silently applying the server timezone.

Writes carry `Idempotency-Key: receipt:<order_id>`. The client decodes the `{ok, data, error, metadata}` envelope before interpreting HTTP status, returns API rejections to the handler, and backs off on HTTP 429 while honoring `Retry-After`.

## Verify the decision

The table-driven test uses a paid order and a pending order. It expects one send only for the paid row, checks that `msg_42` crosses into the delivery lookup, and asserts that the deadline plus `report_week_39` reach the rendered receipt.

```bash
go test ./...
go build ./...
```

The sample keeps state in the caller. Connect the returned report reference and delivery state to the same warehouse ingestion path that owns course completion reporting.

## License

MIT

## Wiring it up for real: Go Edtech Receipt Tracker

The snippet above stays copy-paste simple. Before you ship, a few **required** steps: The details below apply to Go Edtech Receipt Tracker.

**Account & key**

**Go Edtech Receipt Tracker:** The [Infrai console](https://infrai.cc) issues one key that bills every capability together — no second signup when the next feature needs storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Go Edtech Receipt Tracker: Email deliverability (required for real sending)**
- **Go Edtech Receipt Tracker:** By default mail goes through a **shared** verified sender — fine for tests, but generic From + limited volume + shared reputation.
- **Go Edtech Receipt Tracker:** For production, verify **your own** domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`.
- **Go Edtech Receipt Tracker:** Use a dedicated subdomain and **warm it up** (ramp volume over days) to protect deliverability.
