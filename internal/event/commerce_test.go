package event

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fayrly/backend/internal/platform/authn"
)

const testServerKey = "SB-Mid-server-test-key"

func signNotification(serverKey string, n midtransNotification) string {
	sum := sha512.Sum512([]byte(n.OrderID + n.StatusCode + n.GrossAmount + serverKey))
	return hex.EncodeToString(sum[:])
}

func TestNewRouterRegistersPaymentRoutes(t *testing.T) {
	tokens, err := authn.NewManager(strings.Repeat("a", 32), "fayrly", "fayrly-api", time.Minute)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if NewRouter(NewHandler(nil), tokens, func(context.Context) error { return nil }) == nil {
		t.Fatal("NewRouter returned nil")
	}
}

func TestVerifyNotificationUsesServerKey(t *testing.T) {
	midtrans := NewMidtrans(MidtransConfig{ServerKey: testServerKey})
	notification := midtransNotification{
		OrderID:           "order-1",
		StatusCode:        "200",
		GrossAmount:       "150000.00",
		TransactionStatus: "settlement",
		SignatureKey:      signNotification(testServerKey, midtransNotification{OrderID: "order-1", StatusCode: "200", GrossAmount: "150000.00"}),
	}
	if !midtrans.VerifyNotification(notification) {
		t.Fatal("notification signed with the server key must be accepted")
	}

	notification.SignatureKey = signNotification("another-key", midtransNotification{OrderID: "order-1", StatusCode: "200", GrossAmount: "150000.00"})
	if midtrans.VerifyNotification(notification) {
		t.Fatal("notification signed with a different key must be rejected")
	}

	notification.SignatureKey = "not-hex"
	if midtrans.VerifyNotification(notification) {
		t.Fatal("malformed signature must be rejected")
	}
}

func TestVerifyNotificationRejectsMissingFields(t *testing.T) {
	midtrans := NewMidtrans(MidtransConfig{ServerKey: testServerKey})
	if midtrans.VerifyNotification(midtransNotification{}) {
		t.Fatal("empty notification must be rejected")
	}
}

func TestCreateSnap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/snap/v1/transactions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != testServerKey || password != "" {
			t.Errorf("unexpected basic auth: %q / %q", user, password)
		}
		var payload snapRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.TransactionDetails.OrderID != "order-1" || payload.TransactionDetails.GrossAmount != 300000 {
			t.Errorf("unexpected transaction details: %+v", payload.TransactionDetails)
		}
		if len(payload.ItemDetails) != 1 || payload.ItemDetails[0].Price != 150000 || payload.ItemDetails[0].Quantity != 2 {
			t.Errorf("unexpected item details: %+v", payload.ItemDetails)
		}
		if len([]rune(payload.ItemDetails[0].Name)) != 50 {
			t.Errorf("Snap item name must be capped at 50 characters, got %d", len([]rune(payload.ItemDetails[0].Name)))
		}
		want := []string{"gopay", "bank_transfer", "other_qris"}
		if strings.Join(payload.EnabledPayments, ",") != strings.Join(want, ",") {
			t.Errorf("unexpected enabled payments: %v", payload.EnabledPayments)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"token":"snap-token","redirect_url":"https://app.sandbox.midtrans.com/snap/v2/vtweb/snap-token"}`)
	}))
	defer server.Close()

	midtrans := NewMidtrans(MidtransConfig{ServerKey: testServerKey, BaseURL: server.URL})
	snap, err := midtrans.CreateSnap(context.Background(), &TicketOrder{
		ID: "order-1", TicketTypeID: "ticket-1", TicketName: strings.Repeat("Ticket ", 10),
		UnitPrice: 150000, Quantity: 2, GrossAmount: 300000,
	})
	if err != nil {
		t.Fatalf("CreateSnap: %v", err)
	}
	if snap.Token != "snap-token" || snap.RedirectURL == "" {
		t.Fatalf("unexpected snap response: %+v", snap)
	}
}

func TestCreateSnapErrors(t *testing.T) {
	order := &TicketOrder{ID: "order-1", TicketTypeID: "ticket-1", TicketName: "General", UnitPrice: 1000, Quantity: 1, GrossAmount: 1000}

	t.Run("upstream failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error_messages":["server key is invalid"]}`, http.StatusUnauthorized)
		}))
		defer server.Close()
		midtrans := NewMidtrans(MidtransConfig{ServerKey: testServerKey, BaseURL: server.URL})
		if _, err := midtrans.CreateSnap(context.Background(), order); err == nil {
			t.Fatal("expected an error for a non-2xx response")
		}
	})

	t.Run("missing token", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"token":"","redirect_url":""}`)
		}))
		defer server.Close()
		midtrans := NewMidtrans(MidtransConfig{ServerKey: testServerKey, BaseURL: server.URL})
		if _, err := midtrans.CreateSnap(context.Background(), order); err == nil {
			t.Fatal("expected an error when token or redirect URL is missing")
		}
	})
}

func TestIsSuccessfulNotification(t *testing.T) {
	cases := []struct {
		name string
		n    midtransNotification
		want bool
	}{
		{"settlement", midtransNotification{StatusCode: "200", TransactionStatus: "settlement"}, true},
		{"capture accepted", midtransNotification{StatusCode: "200", TransactionStatus: "capture", FraudStatus: "accept"}, true},
		{"capture denied", midtransNotification{StatusCode: "200", TransactionStatus: "capture", FraudStatus: "deny"}, false},
		{"pending", midtransNotification{StatusCode: "201", TransactionStatus: "pending"}, false},
		{"expire", midtransNotification{StatusCode: "407", TransactionStatus: "expire"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSuccessfulNotification(tc.n); got != tc.want {
				t.Fatalf("isSuccessfulNotification() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCreateChargeMethods(t *testing.T) {
	order := &TicketOrder{ID: "order-1", TicketTypeID: "ticket-1", TicketName: "General", UnitPrice: 150000, Quantity: 2, GrossAmount: 300000}

	cases := []struct {
		method      string
		paymentType string
		bank        string
		response    string
		assertPrice func(*testing.T, PaymentInstructions)
	}{
		{
			method:      "bca_va",
			paymentType: "bank_transfer",
			bank:        "bca",
			response:    `{"status_code":"201","transaction_id":"tx-1","payment_type":"bank_transfer","expiry_time":"2026-11-28 10:00:00","va_numbers":[{"bank":"bca","va_number":"12345678901"}]}`,
			assertPrice: func(t *testing.T, got PaymentInstructions) {
				if got.VANumber != "12345678901" || got.Bank != "bca" {
					t.Errorf("unexpected VA instructions: %+v", got)
				}
				if got.ExpiresAt == nil || got.ExpiresAt.Location() != time.UTC {
					t.Errorf("expiry must be parsed into UTC, got %+v", got.ExpiresAt)
				}
			},
		},
		{
			method:      "permata_va",
			paymentType: "bank_transfer",
			bank:        "permata",
			response:    `{"status_code":"201","transaction_id":"tx-2","payment_type":"bank_transfer","permata_va_number":"987654321"}`,
			assertPrice: func(t *testing.T, got PaymentInstructions) {
				if got.VANumber != "987654321" || got.Bank != "permata" {
					t.Errorf("unexpected Permata instructions: %+v", got)
				}
			},
		},
		{
			method:      "qris",
			paymentType: "qris",
			response:    `{"status_code":"201","transaction_id":"tx-3","payment_type":"qris","actions":[{"name":"generate-qr-code","url":"https://api.sandbox.midtrans.com/qr/qr-1"}]}`,
			assertPrice: func(t *testing.T, got PaymentInstructions) {
				if got.QRURL != "https://api.sandbox.midtrans.com/qr/qr-1" {
					t.Errorf("unexpected QR instructions: %+v", got)
				}
			},
		},
		{
			method:      "gopay",
			paymentType: "gopay",
			response:    `{"status_code":"201","transaction_id":"tx-4","payment_type":"gopay","actions":[{"name":"deeplink-redirect","url":"https://gojek.link/pay"},{"name":"generate-qr-code","url":"https://api.sandbox.midtrans.com/qr/qr-2"}]}`,
			assertPrice: func(t *testing.T, got PaymentInstructions) {
				if got.DeeplinkURL != "https://gojek.link/pay" || got.QRURL != "https://api.sandbox.midtrans.com/qr/qr-2" {
					t.Errorf("unexpected GoPay instructions: %+v", got)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v2/charge" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				user, password, ok := r.BasicAuth()
				if !ok || user != testServerKey || password != "" {
					t.Errorf("unexpected basic auth: %q / %q", user, password)
				}
				var payload coreChargeRequest
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode payload: %v", err)
				}
				if payload.PaymentType != tc.paymentType {
					t.Errorf("payment_type = %q, want %q", payload.PaymentType, tc.paymentType)
				}
				if tc.bank != "" && (payload.BankTransfer == nil || payload.BankTransfer.Bank != tc.bank) {
					t.Errorf("unexpected bank_transfer: %+v", payload.BankTransfer)
				}
				if tc.method == "qris" && (payload.QRIS == nil || payload.QRIS.Acquirer != "gopay") {
					t.Errorf("unexpected qris config: %+v", payload.QRIS)
				}
				if payload.TransactionDetails.OrderID != "order-1" || payload.TransactionDetails.GrossAmount != 300000 {
					t.Errorf("unexpected transaction details: %+v", payload.TransactionDetails)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()

			midtrans := NewMidtrans(MidtransConfig{ServerKey: testServerKey, CoreBaseURL: server.URL})
			got, err := midtrans.CreateCharge(context.Background(), order, tc.method)
			if err != nil {
				t.Fatalf("CreateCharge: %v", err)
			}
			if got.PaymentType != tc.paymentType {
				t.Errorf("payment type = %q, want %q", got.PaymentType, tc.paymentType)
			}
			tc.assertPrice(t, got)
		})
	}
}

func TestCreateChargeRejectsUnsupportedMethod(t *testing.T) {
	midtrans := NewMidtrans(MidtransConfig{ServerKey: testServerKey})
	order := &TicketOrder{ID: "order-1", TicketTypeID: "ticket-1", TicketName: "General", UnitPrice: 1000, Quantity: 1, GrossAmount: 1000}
	if _, err := midtrans.CreateCharge(context.Background(), order, "credit_card"); err == nil {
		t.Fatal("expected an error for an unsupported method")
	}
}

func TestCreateChargeRequiresInstruction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status_code":"201","transaction_id":"tx-1","payment_type":"bank_transfer"}`)
	}))
	defer server.Close()
	midtrans := NewMidtrans(MidtransConfig{ServerKey: testServerKey, CoreBaseURL: server.URL})
	order := &TicketOrder{ID: "order-1", TicketTypeID: "ticket-1", TicketName: "General", UnitPrice: 1000, Quantity: 1, GrossAmount: 1000}
	if _, err := midtrans.CreateCharge(context.Background(), order, "bca_va"); err == nil {
		t.Fatal("expected an error when the VA number is missing")
	}
}

func TestFetchStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/order-1/status" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if user, _, ok := r.BasicAuth(); !ok || user != testServerKey {
			t.Errorf("unexpected basic auth: %q", user)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status_code":"200","transaction_id":"tx-9","transaction_status":"settlement","fraud_status":"accept","payment_type":"bank_transfer","gross_amount":"300000.00"}`)
	}))
	defer server.Close()

	midtrans := NewMidtrans(MidtransConfig{ServerKey: testServerKey, CoreBaseURL: server.URL})
	notification, err := midtrans.FetchStatus(context.Background(), "order-1")
	if err != nil {
		t.Fatalf("FetchStatus: %v", err)
	}
	if notification.StatusCode != "200" || notification.TransactionStatus != "settlement" || notification.GrossAmount != "300000.00" {
		t.Fatalf("unexpected notification: %+v", notification)
	}
	if !isSuccessfulNotification(notification) {
		t.Fatal("settled transaction must be treated as successful")
	}
}

func TestFetchStatusErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"status_code":"404","status_message":"Transaction doesn't exist."}`)
	}))
	defer server.Close()
	midtrans := NewMidtrans(MidtransConfig{ServerKey: testServerKey, CoreBaseURL: server.URL})
	if _, err := midtrans.FetchStatus(context.Background(), "missing"); err == nil {
		t.Fatal("expected an error for an unknown transaction")
	}
}
