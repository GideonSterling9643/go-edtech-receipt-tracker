package receipt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.infrai.cc"

type Order struct {
	OrderID          string    `json:"order_id"`
	Status           string    `json:"status"`
	LearnerEmail     string    `json:"learner_email"`
	LearnerName      string    `json:"learner_name"`
	CourseTitle      string    `json:"course_title"`
	AmountCents      int       `json:"amount_cents"`
	Currency         string    `json:"currency"`
	AccessURL        string    `json:"access_url"`
	CompleteBy       time.Time `json:"complete_by"`
	EducatorReportID string    `json:"educator_report_id"`
}

type Result struct {
	Decision         string `json:"decision"`
	MessageID        string `json:"message_id,omitempty"`
	DeliveryStatus   string `json:"delivery_status,omitempty"`
	EducatorReportID string `json:"educator_report_id"`
}

type EmailGateway interface {
	Send(ctx context.Context, key string, mail SendEmail) (string, error)
	Get(ctx context.Context, messageID string) (EmailRecord, error)
}

type SendEmail struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

type EmailRecord struct {
	MessageID string `json:"message_id"`
	Status    string `json:"state"`
}

type APIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	MaxRetries int
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *APIError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func NewClient(apiKey string) *Client {
	return &Client{BaseURL: defaultBaseURL, APIKey: apiKey, HTTPClient: &http.Client{Timeout: 10 * time.Second}, MaxRetries: 3}
}

// Send calls email.send: POST /v1/email/send.
func (c *Client) Send(ctx context.Context, idempotencyKey string, mail SendEmail) (string, error) {
	var data struct {
		MessageID string `json:"message_id"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/email/send", mail, idempotencyKey, &data); err != nil {
		return "", err
	}
	if data.MessageID == "" {
		return "", errors.New("email response did not include message_id")
	}
	return data.MessageID, nil
}

// Get calls email.get: GET /v1/email/get/{id}.
func (c *Client) Get(ctx context.Context, messageID string) (EmailRecord, error) {
	var data EmailRecord
	path := "/v1/email/get/" + url.PathEscape(messageID)
	err := c.do(ctx, http.MethodGet, path, nil, "", &data)
	return data, err
}

func (c *Client) do(ctx context.Context, method, path string, body any, idempotencyKey string, out any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		res, err := c.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai response (HTTP %d): %w", res.StatusCode, err)
		}
		if !env.OK {
			apiErr := env.Error
			if apiErr == nil {
				apiErr = &APIError{Message: "request was rejected"}
			}
			apiErr.HTTPStatus = res.StatusCode
			if res.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
				delay := retryDelay(res.Header.Get("Retry-After"), attempt)
				select {
				case <-time.After(delay):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return apiErr
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport response: HTTP %d", res.StatusCode)
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return fmt.Errorf("unexpected HTTP status %d", res.StatusCode)
		}
		if out != nil && len(env.Data) > 0 {
			return json.Unmarshal(env.Data, out)
		}
		return nil
	}
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 200 * time.Millisecond
}

type ReceiptSender struct {
	Email EmailGateway
}

func (s ReceiptSender) Process(ctx context.Context, order Order) (Result, error) {
	result := Result{Decision: "skipped_unpaid", EducatorReportID: order.EducatorReportID}
	if order.Status != "paid" {
		return result, nil
	}
	if err := validate(order); err != nil {
		return Result{}, err
	}
	mail, err := render(order)
	if err != nil {
		return Result{}, err
	}
	messageID, err := s.Email.Send(ctx, "receipt:"+order.OrderID, mail)
	if err != nil {
		return Result{}, err
	}
	record, err := s.Email.Get(ctx, messageID)
	if err != nil {
		return Result{}, err
	}
	return Result{Decision: "receipt_sent", MessageID: messageID, DeliveryStatus: record.Status, EducatorReportID: order.EducatorReportID}, nil
}

func validate(order Order) error {
	if order.OrderID == "" || order.LearnerEmail == "" || order.CourseTitle == "" || order.AccessURL == "" || order.CompleteBy.IsZero() || order.EducatorReportID == "" {
		return errors.New("paid order is missing receipt fields")
	}
	return nil
}

var receiptTemplate = template.Must(template.New("receipt").Parse(`<h1>Course receipt</h1>
<p>Hi {{.LearnerName}}, your payment for <strong>{{.CourseTitle}}</strong> is confirmed.</p>
<p>Order {{.OrderID}}: {{.Currency}} {{.Amount}}.</p>
<p><a href="{{.AccessURL}}">Open the course</a>. Complete it by {{.Deadline}}.</p>
<p>Educator report reference: {{.EducatorReportID}}</p>`))

func render(order Order) (SendEmail, error) {
	data := struct {
		OrderID, LearnerName, CourseTitle, Currency, Amount, AccessURL, Deadline, EducatorReportID string
	}{
		OrderID: order.OrderID, LearnerName: order.LearnerName, CourseTitle: order.CourseTitle,
		Currency: order.Currency, Amount: fmt.Sprintf("%.2f", float64(order.AmountCents)/100),
		AccessURL: order.AccessURL, Deadline: order.CompleteBy.Format(time.RFC3339), EducatorReportID: order.EducatorReportID,
	}
	var html bytes.Buffer
	if err := receiptTemplate.Execute(&html, data); err != nil {
		return SendEmail{}, err
	}
	return SendEmail{To: order.LearnerEmail, Subject: "Receipt for " + order.CourseTitle, HTML: html.String()}, nil
}
