package payments

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"discord-bot/config"
	"discord-bot/storage"
)

func (svc *Service) runWebhookServer() {
	port := svc.cfg.Payment.Webhook.Port
	apiURL := svc.cfg.Payment.Webhook.APIURL

	mux := http.NewServeMux()

	if svc.paypal != nil && svc.cfg.Payment.PayPal.PaymentNotifications.Type == "webhook" {
		mux.HandleFunc("/ipn/paypal", svc.handlePayPalWebhook)
		slog.Info("PayPal webhook registered", "url", fmt.Sprintf("%s/ipn/paypal", apiURL))
	}
	if svc.stripe != nil && svc.cfg.Payment.Stripe.PaymentNotifications.Type == "webhook" {
		mux.HandleFunc("/webhook/stripe", svc.handleStripeWebhook)
		slog.Info("Stripe webhook registered", "url", fmt.Sprintf("%s/webhook/stripe", apiURL))
	}
	if svc.coinbase != nil && svc.cfg.Payment.Coinbase.PaymentNotifications.Type == "webhook" {
		mux.HandleFunc("/webhook/coinbase", svc.handleCoinbaseWebhook)
		slog.Info("Coinbase webhook registered", "url", fmt.Sprintf("%s/webhook/coinbase", apiURL))
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		<-svc.ctx.Done()
		_ = server.Shutdown(svc.ctx)
	}()

	slog.Info("Webhook server listening", "port", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Webhook server error", "error", err)
	}
}

func (svc *Service) handlePayPalWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	var event struct {
		EventType string `json:"event_type"`
		Resource  struct {
			ID string `json:"id"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	slog.Info("PayPal webhook event", "type", event.EventType)
	if event.EventType == "INVOICING.INVOICE.PAID" {
		svc.webhookConfirm("paypal", event.Resource.ID, svc.cfg.Payment.PayPal.Name)
	}
	w.WriteHeader(200)
}

func (svc *Service) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	secret := svc.cfg.Payment.Stripe.PaymentNotifications.WebhookSigningSecret
	if secret != "" {
		sig := r.Header.Get("Stripe-Signature")
		if !verifyStripeSignature(body, sig, secret) {
			slog.Warn("Stripe webhook signature verification failed")
			http.Error(w, "unauthorized", 401)
			return
		}
	}

	var event struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID string `json:"id"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	slog.Info("Stripe webhook event", "type", event.Type)
	if event.Type == "checkout.session.completed" {
		svc.webhookConfirm("stripe", event.Data.Object.ID, svc.cfg.Payment.Stripe.Name)
	}
	w.WriteHeader(200)
}

func (svc *Service) handleCoinbaseWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	secret := svc.cfg.Payment.Coinbase.PaymentNotifications.WebhookSharedSecret
	if secret != "" {
		sig := r.Header.Get("X-CC-Webhook-Signature")
		if !verifyCoinbaseSignature(body, sig, secret) {
			slog.Warn("Coinbase webhook signature verification failed")
			http.Error(w, "unauthorized", 401)
			return
		}
	}

	var event struct {
		Event struct {
			Type string `json:"type"`
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	slog.Info("Coinbase webhook event", "type", event.Event.Type)
	if event.Event.Type == "charge:confirmed" {
		svc.webhookConfirm("coinbase", event.Event.Data.ID, svc.cfg.Payment.Coinbase.Name)
	}
	w.WriteHeader(200)
}

func (svc *Service) webhookConfirm(gateway, paymentID, gatewayName string) {
	if gatewayName == "" {
		gatewayName = gateway
	}
	for _, gs := range storage.GetAllGuildStates() {
		gs.Lock()
		var found *config.CommissionInvoice
		for i := range gs.CommissionsRuntime.Invoices {
			inv := &gs.CommissionsRuntime.Invoices[i]
			if inv.Paid {
				continue
			}
			match := false
			switch gateway {
			case "paypal":
				match = inv.PayPalInvoiceID == paymentID
			case "stripe":
				match = inv.StripeSessionID == paymentID
			case "coinbase":
				match = inv.CoinbaseChargeID == paymentID
			}
			if match {
				inv.Paid = true
				cp := *inv
				found = &cp
				break
			}
		}
		gs.Unlock()

		if found != nil {
			if err := gs.Save(); err != nil {
				slog.Warn("Failed to save guild state after webhook confirm", "gateway", gateway, "payment_id", paymentID, "error", err)
			}
			svc.notifyPaid(*found, gatewayName)
			return
		}
	}
	slog.Warn("No matching invoice for webhook payment", "gateway", gateway, "payment_id", paymentID)
}

func verifyStripeSignature(payload []byte, sigHeader, secret string) bool {
	var ts, sig string
	for _, part := range strings.Split(sigHeader, ",") {
		switch {
		case strings.HasPrefix(part, "t="):
			ts = strings.TrimPrefix(part, "t=")
		case strings.HasPrefix(part, "v1="):
			sig = strings.TrimPrefix(part, "v1=")
		}
	}
	if ts == "" || sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(payload)))
	return hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(sig))
}

func verifyCoinbaseSignature(payload []byte, sigHeader, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(sigHeader))
}
