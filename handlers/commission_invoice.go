package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"discord-bot/config"
	"discord-bot/payments"
	"discord-bot/storage"

	"github.com/bwmarrin/discordgo"
)

func handleInvoiceCreate(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	om := subOptMap(opts)
	cfg := storage.Cfg
	gs := storage.GetGuild(i.GuildID)

	client := om["client"].UserValue(s)
	amount := om["amount"].FloatValue()
	description := om["description"].StringValue()
	currency := strings.ToUpper(optStr(om, "currency", "USD"))
	note := optStr(om, "note", "")

	hasGateway := payments.Svc != nil
	paypalEmail := config.EffectiveCommissionPayPalEmail(cfg, gs)
	paypalMe := config.EffectiveCommissionPayPalMe(cfg, gs)
	if !hasGateway && paypalEmail == "" && paypalMe == "" {
		respond(s, i, "❌ No payment method configured. Set up a gateway via `/commission setup` or the `payment` block in `config.json`.", true)
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})

	gs.Lock()
	gs.CommissionsRuntime.InvoiceCounter++
	invNum := gs.CommissionsRuntime.InvoiceCounter
	gs.Unlock()

	inv := config.CommissionInvoice{
		Number:      invNum,
		ChannelID:   i.ChannelID,
		GuildID:     i.GuildID,
		ClientID:    client.ID,
		CreatedBy:   i.Member.User.ID,
		Amount:      amount,
		Currency:    currency,
		Description: description,
		Note:        note,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	gatewayButtons := buildInvoiceButtons(&inv, paypalMe, amount, currency)

	gs.Lock()
	gs.CommissionsRuntime.Invoices = append(gs.CommissionsRuntime.Invoices, inv)
	gs.Unlock()
	_ = gs.Save()

	fields := []*discordgo.MessageEmbedField{
		{Name: "Invoice #", Value: fmt.Sprintf("`INV-%04d`", invNum), Inline: true},
		{Name: "Client", Value: fmt.Sprintf("<@%s>", client.ID), Inline: true},
		{Name: "Amount", Value: fmt.Sprintf("**%.2f %s**", amount, currency), Inline: true},
		{Name: "Service", Value: description, Inline: false},
	}
	if len(gatewayButtons) == 0 && paypalEmail != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Pay To (PayPal)", Value: fmt.Sprintf("`%s`", paypalEmail), Inline: true,
		})
	}
	if note != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Note", Value: note, Inline: false})
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("🧾 Invoice #INV-%04d", invNum),
		Description: fmt.Sprintf("<@%s> — please review and complete payment below.", client.ID),
		Color:       0xF0A500,
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Created by %s • %s", i.Member.User.Username, time.Now().Format("Jan 2, 2006"))},
		Timestamp:   time.Now().Format(time.RFC3339),
	}

	send := buildInvoiceSend(fmt.Sprintf("<@%s>", client.ID), embed, gatewayButtons)
	if _, err := s.ChannelMessageSendComplex(i.ChannelID, send); err != nil {
		_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: fmt.Sprintf("❌ Failed to post invoice: %s", err.Error()),
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		return
	}

	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: fmt.Sprintf("✅ Invoice `INV-%04d` posted for <@%s> — **%.2f %s**.", invNum, client.ID, amount, currency),
		Flags:   discordgo.MessageFlagsEphemeral,
	})
}

func handleInvoiceList(s *discordgo.Session, i *discordgo.InteractionCreate) {
	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	invs := gs.CommissionsRuntime.Invoices
	gs.Unlock()

	if len(invs) == 0 {
		respond(s, i, "📭 No invoices on record.", true)
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Invoices** (%d total):\n\n", len(invs)))
	for _, inv := range invs {
		paid := "unpaid"
		if inv.Paid {
			paid = "✅ paid"
		}
		sb.WriteString(fmt.Sprintf("`INV-%04d` — <@%s> — **%.2f %s** — %s\n", inv.Number, inv.ClientID, inv.Amount, inv.Currency, paid))
	}
	respond(s, i, sb.String(), true)
}

func handleCommissionInvoiceButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := strings.TrimPrefix(i.MessageComponentData().CustomID, "commission_invoice_btn:")

	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	ct, ok := gs.CommissionsRuntime.OpenCommissions[channelID]
	gs.Unlock()
	if !ok {
		respond(s, i, "❌ Could not find the commission data for this channel.", true)
		return
	}

	descDefault := fmt.Sprintf("%s — %s", ct.ServiceName, ct.Details)
	if len(descDefault) > 100 {
		descDefault = descDefault[:100]
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "commission_invoice_modal:" + channelID,
			Title:    fmt.Sprintf("Issue Invoice — Commission #%04d", ct.Number),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "amount",
						Label:       "Amount to charge",
						Style:       discordgo.TextInputShort,
						Required:    true,
						Placeholder: "e.g. 50.00",
						MaxLength:   20,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "currency",
						Label:       "Currency (default: EUR)",
						Style:       discordgo.TextInputShort,
						Required:    false,
						Placeholder: "EUR",
						MaxLength:   5,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:  "description",
						Label:     "Invoice description (pre-filled)",
						Style:     discordgo.TextInputParagraph,
						Required:  true,
						Value:     descDefault,
						MaxLength: 500,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "note",
						Label:       "Additional note (optional)",
						Style:       discordgo.TextInputShort,
						Required:    false,
						Placeholder: "e.g. Due in 7 days",
						MaxLength:   200,
					},
				}},
			},
		},
	})
}

func handleCommissionInvoiceModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	channelID := strings.TrimPrefix(data.CustomID, "commission_invoice_modal:")

	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	ct, ok := gs.CommissionsRuntime.OpenCommissions[channelID]
	gs.Unlock()
	if !ok {
		respond(s, i, "❌ Could not find the commission data. The ticket may have been closed.", true)
		return
	}

	modalFields := modalTextValues(data.Components)
	amountStr := strings.TrimSpace(modalFields["amount"])
	currency := strings.TrimSpace(modalFields["currency"])
	description := strings.TrimSpace(modalFields["description"])
	note := strings.TrimSpace(modalFields["note"])

	amountStr = strings.ReplaceAll(amountStr, ",", ".")
	amount, parseErr := strconv.ParseFloat(amountStr, 64)
	if parseErr != nil || amount <= 0 {
		respond(s, i, "❌ Invalid amount. Please enter a number like `50.00` or `50,00`.", true)
		return
	}
	if currency == "" {
		currency = "EUR"
	}
	currency = strings.ToUpper(currency)
	if description == "" {
		description = ct.ServiceName
	}

	cfg := storage.Cfg
	paypalEmail := config.EffectiveCommissionPayPalEmail(cfg, gs)
	paypalMe := config.EffectiveCommissionPayPalMe(cfg, gs)
	if payments.Svc == nil && paypalEmail == "" && paypalMe == "" {
		respond(s, i, "❌ No payment method configured.", true)
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})

	gs.Lock()
	gs.CommissionsRuntime.InvoiceCounter++
	invNum := gs.CommissionsRuntime.InvoiceCounter
	gs.Unlock()

	inv := config.CommissionInvoice{
		Number:      invNum,
		ChannelID:   channelID,
		GuildID:     i.GuildID,
		ClientID:    ct.UserID,
		CreatedBy:   i.Member.User.ID,
		Amount:      amount,
		Currency:    currency,
		Description: description,
		Note:        note,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	gatewayButtons := buildInvoiceButtons(&inv, paypalMe, amount, currency)

	gs.Lock()
	gs.CommissionsRuntime.Invoices = append(gs.CommissionsRuntime.Invoices, inv)
	gs.Unlock()
	_ = gs.Save()

	fields := []*discordgo.MessageEmbedField{
		{Name: "Invoice #", Value: fmt.Sprintf("`INV-%04d`", invNum), Inline: true},
		{Name: "Client", Value: fmt.Sprintf("<@%s>", ct.UserID), Inline: true},
		{Name: "Amount", Value: fmt.Sprintf("**%.2f %s**", amount, currency), Inline: true},
		{Name: "Service", Value: ct.ServiceName, Inline: true},
		{Name: "Description", Value: description, Inline: false},
	}
	if len(gatewayButtons) == 0 && paypalEmail != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Pay To (PayPal)", Value: fmt.Sprintf("`%s`", paypalEmail), Inline: true,
		})
	}
	if note != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Note", Value: note, Inline: false})
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("🧾 Invoice #INV-%04d", invNum),
		Description: fmt.Sprintf("<@%s> — please review your order and complete payment below.", ct.UserID),
		Color:       0xF0A500,
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Issued by %s • %s", i.Member.User.Username, time.Now().Format("Jan 2, 2006"))},
		Timestamp:   time.Now().Format(time.RFC3339),
	}

	send := buildInvoiceSend(fmt.Sprintf("<@%s>", ct.UserID), embed, gatewayButtons)
	if _, err := s.ChannelMessageSendComplex(channelID, send); err != nil {
		_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Content: fmt.Sprintf("❌ Failed to post invoice: %s", err.Error()),
			Flags:   discordgo.MessageFlagsEphemeral,
		})
		return
	}

	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: fmt.Sprintf("✅ Invoice `INV-%04d` posted — **%.2f %s** for <@%s>.", invNum, amount, currency, ct.UserID),
		Flags:   discordgo.MessageFlagsEphemeral,
	})
}

// buildInvoiceButtons assembles payment link buttons from the gateway and/or PayPal.me fallback.
func buildInvoiceButtons(inv *config.CommissionInvoice, paypalMe string, amount float64, currency string) []discordgo.MessageComponent {
	var buttons []discordgo.MessageComponent
	if payments.Svc != nil {
		for _, btn := range payments.Svc.CreateLinks(inv) {
			buttons = append(buttons, discordgo.Button{
				Label: btn.Label,
				Style: discordgo.LinkButton,
				URL:   btn.URL,
				Emoji: &discordgo.ComponentEmoji{Name: btn.Emoji},
			})
		}
	}
	if len(buttons) == 0 && paypalMe != "" {
		paypalURL := fmt.Sprintf("https://paypal.me/%s/%.2f%s", paypalMe, amount, currency)
		buttons = append(buttons, discordgo.Button{
			Label: fmt.Sprintf("Pay %.2f %s via PayPal", amount, currency),
			Style: discordgo.LinkButton,
			URL:   paypalURL,
			Emoji: &discordgo.ComponentEmoji{Name: "💳"},
		})
	}
	return buttons
}

// buildInvoiceSend creates the MessageSend with the embed and button rows (max 5 per row).
func buildInvoiceSend(content string, embed *discordgo.MessageEmbed, buttons []discordgo.MessageComponent) *discordgo.MessageSend {
	send := &discordgo.MessageSend{
		Content: content,
		Embeds:  []*discordgo.MessageEmbed{embed},
	}
	for start := 0; start < len(buttons); start += 5 {
		end := start + 5
		if end > len(buttons) {
			end = len(buttons)
		}
		send.Components = append(send.Components, discordgo.ActionsRow{
			Components: buttons[start:end],
		})
	}
	return send
}
