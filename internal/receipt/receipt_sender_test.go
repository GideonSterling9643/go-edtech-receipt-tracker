package receipt

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeEmail struct {
	sends   []SendEmail
	getID   string
	sendKey string
}

func (f *fakeEmail) Send(_ context.Context, key string, mail SendEmail) (string, error) {
	f.sendKey = key
	f.sends = append(f.sends, mail)
	return "msg_42", nil
}

func (f *fakeEmail) Get(_ context.Context, messageID string) (EmailRecord, error) {
	f.getID = messageID
	return EmailRecord{MessageID: messageID, Status: "queued"}, nil
}

func TestReceiptDecisionAndDeliveryHandoff(t *testing.T) {
	deadline, err := time.Parse(time.RFC3339, "2026-09-30T17:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		status    string
		decision  string
		sendCount int
	}{
		{name: "paid order sends receipt", status: "paid", decision: "receipt_sent", sendCount: 1},
		{name: "pending order waits", status: "pending", decision: "skipped_unpaid", sendCount: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := &fakeEmail{}
			sender := ReceiptSender{Email: gateway}
			got, err := sender.Process(context.Background(), Order{
				OrderID: "ord_1042", Status: tt.status, LearnerEmail: "learner@example.com", LearnerName: "Ari",
				CourseTitle: "Pipeline Observability", AmountCents: 12900, Currency: "USD",
				AccessURL: "https://courses.example.com/enrollments/1042", CompleteBy: deadline, EducatorReportID: "report_week_39",
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.Decision != tt.decision || len(gateway.sends) != tt.sendCount {
				t.Fatalf("decision=%q sends=%d", got.Decision, len(gateway.sends))
			}
			if tt.sendCount == 1 {
				if gateway.sendKey != "receipt:ord_1042" || gateway.getID != "msg_42" {
					t.Fatalf("handoff key=%q get_id=%q", gateway.sendKey, gateway.getID)
				}
				if !strings.Contains(gateway.sends[0].HTML, "report_week_39") || !strings.Contains(gateway.sends[0].HTML, "2026-09-30T17:00:00&#43;08:00") {
					t.Fatalf("receipt did not preserve reporting reference and learner deadline: %s", gateway.sends[0].HTML)
				}
			}
		})
	}
}
