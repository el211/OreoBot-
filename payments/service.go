package payments

import (
	"context"
	"log/slog"
	"strings"

	"discord-bot/config"

	"github.com/bwmarrin/discordgo"
)

type PaymentButton struct {
	Label string
	URL   string
	Emoji string
}

type Service struct {
	cfg      *config.Config
	session  *discordgo.Session
	ctx      context.Context
	paypal   *paypalClient
	stripe   *stripeClient
	coinbase *coinbaseClient
}

var Svc *Service

func Start(cfg *config.Config, session *discordgo.Session, ctx context.Context) *Service {
	svc := &Service{cfg: cfg, session: session, ctx: ctx}
	Svc = svc

	pp := &cfg.Payment.PayPal
	if pp.Enabled && pp.ClientID != "" && pp.ClientSecret != "" {
		svc.paypal = newPayPalClient(pp)
		sandbox := ""
		if pp.UseSandbox {
			sandbox = " (sandbox)"
		}
		slog.Info("PayPal gateway enabled", "sandbox", sandbox != "", "merchant", pp.MerchantEmail)
	}

	st := &cfg.Payment.Stripe
	if st.Enabled && st.SecretKey != "" {
		svc.stripe = newStripeClient(st)
		slog.Info("Stripe gateway enabled")
	}

	cb := &cfg.Payment.Coinbase
	if cb.Enabled && cb.APIKey != "" {
		svc.coinbase = newCoinbaseClient(cb)
		slog.Info("Coinbase Commerce gateway enabled")
	}

	if svc.hasPollingGateway() {
		go svc.runPolling()
	}
	if cfg.Payment.Webhook.Enabled && cfg.Payment.Webhook.Port > 0 {
		go svc.runWebhookServer()
	}

	return svc
}

func (svc *Service) hasPollingGateway() bool {
	pp := svc.cfg.Payment.PayPal
	if pp.Enabled && pp.ClientID != "" && pp.PaymentNotifications.Type == "polling" {
		return true
	}
	st := svc.cfg.Payment.Stripe
	if st.Enabled && st.SecretKey != "" && st.PaymentNotifications.Type == "polling" {
		return true
	}
	cb := svc.cfg.Payment.Coinbase
	if cb.Enabled && cb.APIKey != "" && cb.PaymentNotifications.Type == "polling" {
		return true
	}
	return false
}

func (svc *Service) CreateLinks(inv *config.CommissionInvoice) []PaymentButton {
	var buttons []PaymentButton

	if svc.paypal != nil {
		invoiceID, payerURL, err := svc.paypal.CreateInvoice(inv)
		if err != nil {
			slog.Error("PayPal CreateInvoice failed", "error", err)
		} else {
			inv.PayPalInvoiceID = invoiceID
			inv.PayPalPayerURL = payerURL
			if payerURL != "" {
				label := svc.cfg.Payment.PayPal.ButtonLabel
				if label == "" {
					label = "Pay with PayPal"
				}
				buttons = append(buttons, PaymentButton{Label: label, URL: payerURL, Emoji: "💳"})
			}
		}
	}

	if svc.stripe != nil {
		sessionID, sessionURL, err := svc.stripe.CreateCheckoutSession(inv)
		if err != nil {
			slog.Error("Stripe CreateCheckoutSession failed", "error", err)
		} else {
			inv.StripeSessionID = sessionID
			inv.StripePaymentURL = sessionURL
			label := svc.cfg.Payment.Stripe.ButtonLabel
			if label == "" {
				label = "Pay with Stripe"
			}
			buttons = append(buttons, PaymentButton{Label: label, URL: sessionURL, Emoji: "💳"})
		}
	}

	if svc.coinbase != nil {
		chargeID, hostedURL, err := svc.coinbase.CreateCharge(inv)
		if err != nil {
			slog.Error("Coinbase CreateCharge failed", "error", err)
		} else {
			inv.CoinbaseChargeID = chargeID
			inv.CoinbaseHostedURL = hostedURL
			label := svc.cfg.Payment.Coinbase.ButtonLabel
			if label == "" {
				label = "Pay with Coinbase"
			}
			buttons = append(buttons, PaymentButton{Label: label, URL: hostedURL, Emoji: "₿"})
		}
	}

	return buttons
}

func ActiveGatewayNames(cfg *config.Config) []string {
	var names []string
	if cfg.Payment.PayPal.Enabled && cfg.Payment.PayPal.ClientID != "" {
		n := cfg.Payment.PayPal.Name
		if n == "" {
			n = "PayPal"
		}
		names = append(names, n)
	}
	if cfg.Payment.Stripe.Enabled && cfg.Payment.Stripe.SecretKey != "" {
		n := cfg.Payment.Stripe.Name
		if n == "" {
			n = "Stripe"
		}
		names = append(names, n)
	}
	if cfg.Payment.Coinbase.Enabled && cfg.Payment.Coinbase.APIKey != "" {
		n := cfg.Payment.Coinbase.Name
		if n == "" {
			n = "Coinbase Commerce"
		}
		names = append(names, n)
	}
	return names
}

func FooterText(cfg *config.Config, gs *config.GuildState) string {
	if gateways := ActiveGatewayNames(cfg); len(gateways) > 0 {
		return "Payments accepted via: " + strings.Join(gateways, " • ")
	}
	email := config.EffectiveCommissionPayPalEmail(cfg, gs)
	if email != "" {
		return "Payments via PayPal • " + email
	}
	return ""
}
