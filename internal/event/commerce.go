package event

import (
	"context"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrTicketNotFound       = errors.New("ticket type not found")
	ErrTicketsUnavailable   = errors.New("not enough tickets available")
	ErrEventCapacityFull    = errors.New("event capacity is full")
	ErrOrderNotFound        = errors.New("ticket order not found")
	ErrPaymentInProgress    = errors.New("payment initialization is still in progress; retry with the same idempotency key")
	ErrUnsupportedCurrency  = errors.New("Midtrans checkout supports IDR only")
	ErrTicketCurrencyLocked = errors.New("event currency cannot change after ticket types are created")
	ErrMidtransUnavailable  = errors.New("Midtrans payment initialization failed")
	ErrTicketTypeExists     = errors.New("ticket type name already exists for this event")
)

type TicketType struct {
	ID          string    `json:"id"`
	EventID     string    `json:"event_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Price       int64     `json:"price"`
	Currency    string    `json:"currency"`
	Capacity    int       `json:"capacity"`
	Available   int       `json:"available"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
}

type CreateTicketTypeRequest struct {
	Name        string `json:"name" validate:"required,min=1,max=120"`
	Description string `json:"description" validate:"omitempty,max=1000"`
	Price       int64  `json:"price" validate:"gt=0"`
	Capacity    int    `json:"capacity" validate:"gt=0,lte=1000000"`
}

type CreateTicketOrderRequest struct {
	TicketTypeID  string `json:"ticket_type_id" validate:"required,uuid"`
	Quantity      int    `json:"quantity" validate:"required,gte=1,lte=10"`
	PaymentMethod string `json:"payment_method" validate:"omitempty,oneof=bca_va bni_va bri_va permata_va qris gopay"`
}

type TicketOrder struct {
	ID            string     `json:"id"`
	EventID       string     `json:"event_id"`
	TicketTypeID  string     `json:"ticket_type_id"`
	UserID        string     `json:"user_id,omitempty"`
	EventTitle    string     `json:"event_title"`
	TicketName    string     `json:"ticket_name"`
	Quantity      int        `json:"quantity"`
	UnitPrice     int64      `json:"unit_price"`
	GrossAmount   int64      `json:"gross_amount"`
	Currency      string     `json:"currency"`
	Status        string     `json:"status"`
	PaymentMethod string     `json:"payment_method,omitempty"`
	PaymentType   string     `json:"payment_type,omitempty"`
	VANumber      string     `json:"va_number,omitempty"`
	QRURL         string     `json:"qr_url,omitempty"`
	DeeplinkURL   string     `json:"deeplink_url,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	SnapToken     string     `json:"snap_token,omitempty"`
	RedirectURL   string     `json:"redirect_url,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type MidtransConfig struct {
	ServerKey   string
	BaseURL     string
	CoreBaseURL string
	Client      *http.Client
}

type Midtrans struct {
	serverKey   string
	baseURL     string
	coreBaseURL string
	client      *http.Client
}

func NewMidtrans(cfg MidtransConfig) *Midtrans {
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = "https://app.sandbox.midtrans.com"
	}
	core := strings.TrimRight(cfg.CoreBaseURL, "/")
	if core == "" {
		core = strings.Replace(base, "app.", "api.", 1)
	}
	return &Midtrans{serverKey: cfg.ServerKey, baseURL: base, coreBaseURL: core, client: client}
}

type snapRequest struct {
	TransactionDetails struct {
		OrderID     string `json:"order_id"`
		GrossAmount int64  `json:"gross_amount"`
	} `json:"transaction_details"`
	ItemDetails     []snapItem `json:"item_details"`
	EnabledPayments []string   `json:"enabled_payments"`
}
type snapItem struct {
	ID       string `json:"id"`
	Price    int64  `json:"price"`
	Quantity int    `json:"quantity"`
	Name     string `json:"name"`
}

type snapResponse struct {
	Token       string `json:"token"`
	RedirectURL string `json:"redirect_url"`
}

func snapItemName(name string) string {
	const limit = 50
	runes := []rune(strings.TrimSpace(name))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}

func (m *Midtrans) CreateSnap(ctx context.Context, order *TicketOrder) (snapResponse, error) {
	var payload snapRequest
	payload.TransactionDetails.OrderID = order.ID
	payload.TransactionDetails.GrossAmount = order.GrossAmount
	payload.ItemDetails = []snapItem{{ID: order.TicketTypeID, Price: order.UnitPrice, Quantity: order.Quantity, Name: snapItemName(order.TicketName)}}
	payload.EnabledPayments = []string{"gopay", "bank_transfer", "other_qris"}
	body, err := json.Marshal(payload)
	if err != nil {
		return snapResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/snap/v1/transactions", strings.NewReader(string(body)))
	if err != nil {
		return snapResponse{}, err
	}
	req.SetBasicAuth(m.serverKey, "")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return snapResponse{}, fmt.Errorf("call Midtrans Snap: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return snapResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return snapResponse{}, fmt.Errorf("Midtrans Snap returned HTTP %d", resp.StatusCode)
	}
	var result snapResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return snapResponse{}, err
	}
	if result.Token == "" || result.RedirectURL == "" {
		return snapResponse{}, errors.New("Midtrans Snap response omitted token or redirect URL")
	}
	return result, nil
}

// PaymentInstructions is the data needed to render a Fayrly-owned checkout page.
type PaymentInstructions struct {
	TransactionID string
	PaymentType   string
	Bank          string
	VANumber      string
	QRURL         string
	DeeplinkURL   string
	ExpiresAt     *time.Time
}

type coreBankTransfer struct {
	Bank string `json:"bank"`
}

type coreQRIS struct {
	Acquirer string `json:"acquirer"`
}

type coreChargeRequest struct {
	PaymentType        string `json:"payment_type"`
	TransactionDetails struct {
		OrderID     string `json:"order_id"`
		GrossAmount int64  `json:"gross_amount"`
	} `json:"transaction_details"`
	ItemDetails  []snapItem        `json:"item_details"`
	BankTransfer *coreBankTransfer `json:"bank_transfer,omitempty"`
	QRIS         *coreQRIS         `json:"qris,omitempty"`
}

type coreAction struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	URL    string `json:"url"`
}

type coreChargeResponse struct {
	StatusCode        string `json:"status_code"`
	StatusMessage     string `json:"status_message"`
	TransactionID     string `json:"transaction_id"`
	TransactionStatus string `json:"transaction_status"`
	FraudStatus       string `json:"fraud_status"`
	PaymentType       string `json:"payment_type"`
	GrossAmount       string `json:"gross_amount"`
	ExpiryTime        string `json:"expiry_time"`
	VANumbers         []struct {
		Bank     string `json:"bank"`
		VANumber string `json:"va_number"`
	} `json:"va_numbers"`
	PermataVANumber string       `json:"permata_va_number"`
	Actions         []coreAction `json:"actions"`
}

var coreBanks = map[string]string{
	"bca_va":     "bca",
	"bni_va":     "bni",
	"bri_va":     "bri",
	"permata_va": "permata",
}

func parseMidtransExpiry(value string) *time.Time {
	if value == "" {
		return nil
	}
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		jakarta = time.FixedZone("WIB", 7*60*60)
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, jakarta)
	if err != nil {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}

// CreateCharge asks the Midtrans Core API for a payment instruction in the
// requested method, so the payment UI can stay inside Fayrly.
func (m *Midtrans) CreateCharge(ctx context.Context, order *TicketOrder, method string) (PaymentInstructions, error) {
	var payload coreChargeRequest
	payload.TransactionDetails.OrderID = order.ID
	payload.TransactionDetails.GrossAmount = order.GrossAmount
	payload.ItemDetails = []snapItem{{ID: order.TicketTypeID, Price: order.UnitPrice, Quantity: order.Quantity, Name: snapItemName(order.TicketName)}}
	switch method {
	case "qris":
		payload.PaymentType = "qris"
		payload.QRIS = &coreQRIS{Acquirer: "gopay"}
	case "gopay":
		payload.PaymentType = "gopay"
	default:
		bank, ok := coreBanks[method]
		if !ok {
			return PaymentInstructions{}, fmt.Errorf("unsupported payment method %q", method)
		}
		payload.PaymentType = "bank_transfer"
		payload.BankTransfer = &coreBankTransfer{Bank: bank}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return PaymentInstructions{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.coreBaseURL+"/v2/charge", strings.NewReader(string(body)))
	if err != nil {
		return PaymentInstructions{}, err
	}
	req.SetBasicAuth(m.serverKey, "")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return PaymentInstructions{}, fmt.Errorf("call Midtrans Core API: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return PaymentInstructions{}, err
	}
	var result coreChargeResponse
	_ = json.Unmarshal(responseBody, &result)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !strings.HasPrefix(result.StatusCode, "2") {
		if result.StatusMessage != "" {
			return PaymentInstructions{}, fmt.Errorf("Midtrans charge rejected: %s", result.StatusMessage)
		}
		return PaymentInstructions{}, fmt.Errorf("Midtrans charge returned HTTP %d", resp.StatusCode)
	}
	instructions := PaymentInstructions{
		TransactionID: result.TransactionID,
		PaymentType:   result.PaymentType,
		ExpiresAt:     parseMidtransExpiry(result.ExpiryTime),
	}
	for _, action := range result.Actions {
		switch action.Name {
		case "generate-qr-code":
			instructions.QRURL = action.URL
		case "deeplink-redirect":
			instructions.DeeplinkURL = action.URL
		}
	}
	if len(result.VANumbers) > 0 {
		instructions.Bank = result.VANumbers[0].Bank
		instructions.VANumber = result.VANumbers[0].VANumber
	} else if result.PermataVANumber != "" {
		instructions.Bank = "permata"
		instructions.VANumber = result.PermataVANumber
	}
	if instructions.PaymentType == "" {
		instructions.PaymentType = payload.PaymentType
	}
	if payload.PaymentType == "bank_transfer" && instructions.VANumber == "" {
		return PaymentInstructions{}, errors.New("Midtrans charge response omitted the virtual account number")
	}
	if payload.PaymentType == "qris" && instructions.QRURL == "" {
		return PaymentInstructions{}, errors.New("Midtrans charge response omitted the QR code")
	}
	return instructions, nil
}

type coreStatusResponse struct {
	StatusCode        string `json:"status_code"`
	StatusMessage     string `json:"status_message"`
	TransactionID     string `json:"transaction_id"`
	TransactionStatus string `json:"transaction_status"`
	FraudStatus       string `json:"fraud_status"`
	PaymentType       string `json:"payment_type"`
	GrossAmount       string `json:"gross_amount"`
}

// FetchStatus reads the authoritative transaction state from the Core API so
// the checkout page can update without waiting for a webhook.
func (m *Midtrans) FetchStatus(ctx context.Context, orderID string) (midtransNotification, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.coreBaseURL+"/v2/"+url.PathEscape(orderID)+"/status", nil)
	if err != nil {
		return midtransNotification{}, err
	}
	req.SetBasicAuth(m.serverKey, "")
	req.Header.Set("Accept", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return midtransNotification{}, fmt.Errorf("call Midtrans status API: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return midtransNotification{}, err
	}
	var result coreStatusResponse
	_ = json.Unmarshal(responseBody, &result)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || result.TransactionStatus == "" {
		if result.StatusMessage != "" {
			return midtransNotification{}, fmt.Errorf("Midtrans status check failed: %s", result.StatusMessage)
		}
		return midtransNotification{}, fmt.Errorf("Midtrans status check returned HTTP %d", resp.StatusCode)
	}
	statusCode := result.StatusCode
	if (result.TransactionStatus == "settlement" || result.TransactionStatus == "capture") &&
		(result.FraudStatus == "" || strings.EqualFold(result.FraudStatus, "accept")) {
		statusCode = "200"
	}
	return midtransNotification{
		OrderID:           orderID,
		StatusCode:        statusCode,
		GrossAmount:       result.GrossAmount,
		TransactionID:     result.TransactionID,
		TransactionStatus: result.TransactionStatus,
		PaymentType:       result.PaymentType,
		FraudStatus:       result.FraudStatus,
	}, nil
}

type midtransNotification struct {
	OrderID           string `json:"order_id"`
	StatusCode        string `json:"status_code"`
	GrossAmount       string `json:"gross_amount"`
	SignatureKey      string `json:"signature_key"`
	TransactionID     string `json:"transaction_id"`
	TransactionStatus string `json:"transaction_status"`
	PaymentType       string `json:"payment_type"`
	FraudStatus       string `json:"fraud_status"`
}

func (m *Midtrans) VerifyNotification(n midtransNotification) bool {
	if m.serverKey == "" || n.OrderID == "" || n.StatusCode == "" || n.GrossAmount == "" || n.SignatureKey == "" {
		return false
	}
	// Midtrans signs notifications with the server key, not a separate secret.
	sum := sha512.Sum512([]byte(n.OrderID + n.StatusCode + n.GrossAmount + m.serverKey))
	expected := sum[:]
	got, err := hex.DecodeString(n.SignatureKey)
	return err == nil && len(got) == len(expected) && subtle.ConstantTimeCompare(got, expected) == 1
}

func isSuccessfulNotification(n midtransNotification) bool {
	return n.StatusCode == "200" && (n.TransactionStatus == "settlement" || n.TransactionStatus == "capture") &&
		(n.FraudStatus == "" || strings.EqualFold(n.FraudStatus, "accept"))
}

func (r *Repository) CreateTicketType(ctx context.Context, eventID string, req CreateTicketTypeRequest) (*TicketType, error) {
	var row TicketType
	result := r.db.WithContext(ctx).Exec(`INSERT INTO event_ticket_types (id,event_id,name,description,price,capacity)
		SELECT ?::uuid,e.id,?,?,?,? FROM events e WHERE e.id=?::uuid AND e.deleted_at IS NULL AND e.status='draft'`,
		uuid.NewString(), strings.TrimSpace(req.Name), strings.TrimSpace(req.Description), req.Price, req.Capacity, eventID)
	if result.Error != nil {
		return nil, mapEventUnique(result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrEventNotFound
	}
	err := r.db.WithContext(ctx).Raw(`SELECT t.id::text AS id,t.event_id::text AS event_id,t.name,t.description,t.price,
		btrim(e.currency) AS currency,t.capacity,t.capacity AS available,t.active,t.created_at
		FROM event_ticket_types t JOIN events e ON e.id=t.event_id WHERE t.event_id=?::uuid AND t.name=?`,
		eventID, strings.TrimSpace(req.Name)).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == "" {
		return nil, ErrEventNotFound
	}
	return &row, nil
}

func (r *Repository) ListTicketTypes(ctx context.Context, eventID string, includeInactive bool) ([]TicketType, error) {
	query := `SELECT t.id::text AS id, t.event_id::text AS event_id, t.name, t.description,
		t.price, btrim(e.currency) AS currency, t.capacity, t.capacity-t.sold-t.reserved AS available,
		t.active, t.created_at FROM event_ticket_types t JOIN events e ON e.id=t.event_id
		WHERE t.event_id=?::uuid AND e.deleted_at IS NULL`
	if !includeInactive {
		query += ` AND t.active=true AND e.status IN ('published','ongoing')`
	}
	query += ` ORDER BY t.price, t.name`
	var rows []TicketType
	err := r.db.WithContext(ctx).Raw(query, eventID).Scan(&rows).Error
	return rows, err
}

func (r *Repository) CreateTicketOrder(ctx context.Context, userID, requestKey, eventID, ticketTypeID string, quantity int) (*TicketOrder, error) {
	var order TicketOrder
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var ticket struct {
			ID            string
			EventID       string
			Name          string
			Price         int64
			Currency      string
			EventTitle    string
			EventCapacity int
		}
		if err := tx.Raw(`SELECT t.id::text AS id,t.event_id::text AS event_id,t.name,t.price,btrim(e.currency) AS currency,e.title AS event_title,e.capacity AS event_capacity
			FROM event_ticket_types t JOIN events e ON e.id=t.event_id
			WHERE t.id=?::uuid AND t.event_id=?::uuid AND t.active=true AND e.status IN ('published','ongoing') AND e.deleted_at IS NULL FOR UPDATE OF e,t`, ticketTypeID, eventID).Scan(&ticket).Error; err != nil {
			return err
		}
		if ticket.ID == "" {
			return ErrTicketNotFound
		}
		if ticket.Currency != "IDR" {
			return ErrUnsupportedCurrency
		}
		var prior TicketOrder
		if err := tx.Raw(`SELECT id::text AS id,event_id::text AS event_id,ticket_type_id::text AS ticket_type_id,user_id::text AS user_id,
			event_title,ticket_name,quantity,unit_price,gross_amount,btrim(currency) AS currency,status,payment_method,payment_type,
			va_number,qr_url,deeplink_url,expires_at,snap_token,redirect_url,created_at,updated_at
			FROM event_ticket_orders WHERE user_id=?::uuid AND request_key=? FOR UPDATE`, userID, requestKey).Scan(&prior).Error; err != nil {
			return err
		}
		resume := false
		if prior.ID != "" {
			switch {
			case prior.Status == "pending" && (prior.RedirectURL != "" || prior.PaymentMethod != ""):
				order = prior
				return nil
			case prior.Status == "pending":
				return ErrPaymentInProgress
			case prior.Status == "failed" && prior.RedirectURL == "":
				// No Snap session or payment instruction exists yet, so the same idempotency key can retry safely.
				resume = true
			default:
				order = prior
				return nil
			}
		}
		id := uuid.NewString()
		result := tx.Exec(`UPDATE event_ticket_types SET reserved=reserved+?,updated_at=now()
			WHERE id=?::uuid AND capacity-sold-reserved >= ?`, quantity, ticketTypeID, quantity)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrTicketsUnavailable
		}
		if ticket.EventCapacity > 0 {
			var inUse int
			if err := tx.Raw(`SELECT coalesce(sum(sold+reserved),0)::int FROM event_ticket_types WHERE event_id=?::uuid`, eventID).Scan(&inUse).Error; err != nil {
				return err
			}
			if inUse > ticket.EventCapacity {
				return ErrEventCapacityFull
			}
		}
		amount := ticket.Price * int64(quantity)
		if amount/int64(quantity) != ticket.Price || amount > 999999999999 {
			return errors.New("ticket total is outside Midtrans amount limits")
		}
		if resume {
			result := tx.Exec(`UPDATE event_ticket_orders SET status='pending',midtrans_transaction_id='',payment_type='',
				snap_token='',redirect_url='',payment_method='',va_number='',qr_url='',deeplink_url='',expires_at=NULL,
				ticket_type_id=?::uuid,event_title=?,ticket_name=?,quantity=?,unit_price=?,
				gross_amount=?,updated_at=now()
				WHERE id=?::uuid AND status='failed' AND redirect_url=''`,
				ticketTypeID, ticket.EventTitle, ticket.Name, quantity, ticket.Price, amount, prior.ID)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return ErrPaymentInProgress
			}
			return tx.Raw(`SELECT id::text AS id,event_id::text AS event_id,ticket_type_id::text AS ticket_type_id,
				user_id::text AS user_id,event_title,ticket_name,quantity,unit_price,gross_amount,btrim(currency) AS currency,
				status,payment_method,payment_type,va_number,qr_url,deeplink_url,expires_at,snap_token,created_at,updated_at
				FROM event_ticket_orders WHERE id=?::uuid`, prior.ID).Scan(&order).Error
		}
		if err := tx.Raw(`INSERT INTO event_ticket_orders (id,request_key,event_id,ticket_type_id,user_id,event_title,ticket_name,quantity,unit_price,gross_amount,currency)
			VALUES (?::uuid,?,?,?::uuid,?::uuid,?,?,?,?,?,?)
			RETURNING id::text AS id,event_id::text AS event_id,ticket_type_id::text AS ticket_type_id,user_id::text AS user_id,
			event_title,ticket_name,quantity,unit_price,gross_amount,btrim(currency) AS currency,status,payment_method,payment_type,
			va_number,qr_url,deeplink_url,expires_at,snap_token,created_at,updated_at`,
			id, requestKey, ticket.EventID, ticketTypeID, userID, ticket.EventTitle, ticket.Name, quantity, ticket.Price, amount, ticket.Currency).Scan(&order).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func (r *Repository) SaveSnap(ctx context.Context, orderID string, snap snapResponse) error {
	return r.db.WithContext(ctx).Exec(`UPDATE event_ticket_orders SET snap_token=?,redirect_url=?,updated_at=now() WHERE id=?::uuid AND status='pending'`, snap.Token, snap.RedirectURL, orderID).Error
}

func (r *Repository) FailOrder(ctx context.Context, orderID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row struct {
			TicketID string
			Quantity int
		}
		res := tx.Raw(`UPDATE event_ticket_orders SET status='failed',updated_at=now() WHERE id=?::uuid AND status='pending' RETURNING ticket_type_id::text AS ticket_id,quantity`, orderID).Scan(&row)
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		return tx.Exec(`UPDATE event_ticket_types SET reserved=greatest(0,reserved-?),updated_at=now() WHERE id=?::uuid`, row.Quantity, row.TicketID).Error
	})
}

func (r *Repository) GetTicketOrder(ctx context.Context, eventID, id, userID string) (*TicketOrder, error) {
	var row TicketOrder
	err := r.db.WithContext(ctx).Raw(`SELECT id::text AS id,event_id::text AS event_id,ticket_type_id::text AS ticket_type_id,
		user_id::text AS user_id,event_title,ticket_name,quantity,unit_price,gross_amount,btrim(currency) AS currency,
		status,payment_method,payment_type,va_number,qr_url,deeplink_url,expires_at,snap_token,redirect_url,created_at,updated_at
		FROM event_ticket_orders
		WHERE id=?::uuid AND event_id=?::uuid AND user_id=?::uuid`, id, eventID, userID).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == "" {
		return nil, ErrOrderNotFound
	}
	return &row, nil
}

func (r *Repository) SavePaymentInstructions(ctx context.Context, orderID, method string, instructions PaymentInstructions) error {
	return r.db.WithContext(ctx).Exec(`UPDATE event_ticket_orders SET payment_method=?,midtrans_transaction_id=?,payment_type=?,
		va_number=?,qr_url=?,deeplink_url=?,expires_at=?,updated_at=now()
		WHERE id=?::uuid AND status='pending'`,
		method, instructions.TransactionID, instructions.PaymentType, instructions.VANumber,
		instructions.QRURL, instructions.DeeplinkURL, instructions.ExpiresAt, orderID).Error
}

func (r *Repository) ApplyNotification(ctx context.Context, n midtransNotification) error {
	status := ""
	switch {
	case isSuccessfulNotification(n):
		status = "paid"
	case strings.EqualFold(n.FraudStatus, "deny"):
		status = "failed"
	case n.TransactionStatus == "expire":
		status = "expired"
	case n.TransactionStatus == "cancel" || n.TransactionStatus == "deny" || n.TransactionStatus == "failure":
		status = "failed"
	case n.TransactionStatus == "pending" || (n.TransactionStatus == "capture" && !strings.EqualFold(n.FraudStatus, "accept")):
		status = "pending"
	default:
		return errors.New("unsupported Midtrans transaction status")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row struct {
			TicketTypeID string
			Quantity     int
			GrossAmount  int64
			Status       string
		}
		if err := tx.Raw(`SELECT ticket_type_id::text AS ticket_type_id,quantity,gross_amount,status FROM event_ticket_orders WHERE id=?::uuid FOR UPDATE`, n.OrderID).Scan(&row).Error; err != nil {
			return err
		}
		if row.TicketTypeID == "" {
			return ErrOrderNotFound
		}
		amount, ok := new(big.Rat).SetString(n.GrossAmount)
		if !ok || amount.Cmp(new(big.Rat).SetInt64(row.GrossAmount)) != 0 {
			return errors.New("Midtrans amount does not match order")
		}
		if row.Status == "paid" || row.Status == "expired" || row.Status == "failed" {
			return nil
		}
		if status == "paid" {
			if err := tx.Exec(`UPDATE event_ticket_types SET reserved=greatest(0,reserved-?),sold=sold+?,updated_at=now() WHERE id=?::uuid`, row.Quantity, row.Quantity, row.TicketTypeID).Error; err != nil {
				return err
			}
		} else if status == "failed" || status == "expired" {
			if err := tx.Exec(`UPDATE event_ticket_types SET reserved=greatest(0,reserved-?),updated_at=now() WHERE id=?::uuid`, row.Quantity, row.TicketTypeID).Error; err != nil {
				return err
			}
		}
		return tx.Exec(`UPDATE event_ticket_orders SET status=?,midtrans_transaction_id=?,payment_type=?,updated_at=now() WHERE id=?::uuid`, status, n.TransactionID, n.PaymentType, n.OrderID).Error
	})
}

func (s *Service) CreateTicketType(ctx context.Context, eventID string, req CreateTicketTypeRequest) (*TicketType, error) {
	return s.repo.CreateTicketType(ctx, eventID, req)
}

func (s *Service) ListTicketTypes(ctx context.Context, eventID string, includeInactive bool) ([]TicketType, error) {
	return s.repo.ListTicketTypes(ctx, eventID, includeInactive)
}

func (s *Service) CreateTicketOrder(ctx context.Context, principalUserID, requestKey, eventID string, req CreateTicketOrderRequest, gateway *Midtrans) (*TicketOrder, error) {
	order, err := s.repo.CreateTicketOrder(ctx, principalUserID, requestKey, eventID, req.TicketTypeID, req.Quantity)
	if err != nil {
		return nil, err
	}
	if req.PaymentMethod != "" {
		if order.PaymentMethod != "" {
			return order, nil
		}
		instructions, err := gateway.CreateCharge(ctx, order, req.PaymentMethod)
		if err != nil {
			_ = s.repo.FailOrder(ctx, order.ID)
			return nil, fmt.Errorf("%w: %v", ErrMidtransUnavailable, err)
		}
		if err := s.repo.SavePaymentInstructions(ctx, order.ID, req.PaymentMethod, instructions); err != nil {
			_ = s.repo.FailOrder(ctx, order.ID)
			return nil, err
		}
		return s.repo.GetTicketOrder(ctx, eventID, order.ID, principalUserID)
	}
	if order.RedirectURL != "" {
		return order, nil
	}
	snap, err := gateway.CreateSnap(ctx, order)
	if err != nil {
		_ = s.repo.FailOrder(ctx, order.ID)
		return nil, fmt.Errorf("%w: %v", ErrMidtransUnavailable, err)
	}
	if err := s.repo.SaveSnap(ctx, order.ID, snap); err != nil {
		_ = s.repo.FailOrder(ctx, order.ID)
		return nil, err
	}
	order.SnapToken = snap.Token
	order.RedirectURL = snap.RedirectURL
	return order, nil
}

func (s *Service) GetTicketOrder(ctx context.Context, eventID, id, userID string) (*TicketOrder, error) {
	return s.repo.GetTicketOrder(ctx, eventID, id, userID)
}

// SyncTicketOrder refreshes a pending order from the Midtrans Core API, which
// keeps the Fayrly checkout page current even without a reachable webhook.
func (s *Service) SyncTicketOrder(ctx context.Context, gateway *Midtrans, eventID, orderID, userID string) (*TicketOrder, error) {
	order, err := s.repo.GetTicketOrder(ctx, eventID, orderID, userID)
	if err != nil {
		return nil, err
	}
	switch order.Status {
	case "paid", "expired", "failed", "cancelled":
		return order, nil
	}
	notification, err := gateway.FetchStatus(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMidtransUnavailable, err)
	}
	if err := s.repo.ApplyNotification(ctx, notification); err != nil {
		return nil, err
	}
	return s.repo.GetTicketOrder(ctx, eventID, orderID, userID)
}

func (s *Service) ApplyPaymentNotification(ctx context.Context, gateway *Midtrans, body io.Reader) error {
	var n midtransNotification
	decoder := json.NewDecoder(io.LimitReader(body, 1<<20))
	if err := decoder.Decode(&n); err != nil {
		return err
	}
	if !gateway.VerifyNotification(n) {
		return errors.New("invalid Midtrans notification signature")
	}
	return s.repo.ApplyNotification(ctx, n)
}

var _ = subtle.ConstantTimeCompare
