package handlers

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"discord-bot/config"
	"discord-bot/payments"
	"discord-bot/storage"

	"github.com/bwmarrin/discordgo"
)


func commissionCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{
			Name:                     "commission",
			Description:              "Commissions system management",
			DefaultMemberPermissions: &adminPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name: "setup", Description: "Configure the commissions system",
					Type: discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionChannel, Name: "channel", Description: "Channel for the commissions panel", Required: true},
						{Type: discordgo.ApplicationCommandOptionString, Name: "paypal-email", Description: "PayPal email address for invoices", Required: true},
						{Type: discordgo.ApplicationCommandOptionString, Name: "paypal-me", Description: "PayPal.me username (without paypal.me/)", Required: false},
						{
							Type: discordgo.ApplicationCommandOptionChannel, Name: "category",
							Description:  "Discord CATEGORY for commission channels (must be a category, not a text channel)",
							Required:     false,
							ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildCategory},
						},
						{Type: discordgo.ApplicationCommandOptionChannel, Name: "log-channel", Description: "Channel for commission logs", Required: false},
						{Type: discordgo.ApplicationCommandOptionString, Name: "staff-roles", Description: "Staff role IDs, comma-separated", Required: false},
					},
				},
				{
					Name: "toggle", Description: "Enable or disable the commissions system",
					Type: discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name: "addservice", Description: "Add a commission service type",
					Type: discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionString, Name: "id", Description: "Short identifier (e.g. plugin)", Required: true},
						{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Display name (e.g. Plugin Development)", Required: true},
						{Type: discordgo.ApplicationCommandOptionString, Name: "emoji", Description: "Emoji (e.g. 🔌)", Required: true},
						{Type: discordgo.ApplicationCommandOptionString, Name: "description", Description: "Short description", Required: true},
						{Type: discordgo.ApplicationCommandOptionString, Name: "starting-price", Description: "Starting price info (e.g. Starting at $20)", Required: false},
					},
				},
				{
					Name: "removeservice", Description: "Remove a commission service type",
					Type: discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionString, Name: "id", Description: "Service ID to remove", Required: true},
					},
				},
				{
					Name: "panel", Description: "Send or refresh the commissions panel",
					Type: discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name: "list", Description: "List all open commissions",
					Type: discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name: "config", Description: "Show the current commissions configuration",
					Type: discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name: "setcategory", Description: "Set the commission category by pasting its ID (right-click category → Copy ID)",
					Type: discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionString, Name: "id", Description: "Category snowflake ID", Required: true},
					},
				},
				{
					Name: "setlogchannel", Description: "Set the commission log channel by pasting its ID",
					Type: discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionString, Name: "id", Description: "Log channel snowflake ID", Required: true},
					},
				},
			},
		},
		{
			Name:                     "invoice",
			Description:              "Create a payment invoice for a commission",
			DefaultMemberPermissions: &adminPerm,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name: "create", Description: "Generate a PayPal invoice for a client",
					Type: discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionUser, Name: "client", Description: "The client to invoice", Required: true},
						{Type: discordgo.ApplicationCommandOptionNumber, Name: "amount", Description: "Invoice total (e.g. 50.00)", Required: true},
						{Type: discordgo.ApplicationCommandOptionString, Name: "description", Description: "Service / work description", Required: true},
						{Type: discordgo.ApplicationCommandOptionString, Name: "currency", Description: "Currency code (default: USD)", Required: false},
						{Type: discordgo.ApplicationCommandOptionString, Name: "note", Description: "Additional note (e.g. Due in 7 days)", Required: false},
					},
				},
				{
					Name:        "list",
					Description: "List all invoices for this server",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
				},
			},
		},
	}
}


func handleCommissionCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	sub := i.ApplicationCommandData().Options[0]
	switch sub.Name {
	case "setup":
		handleCommissionSetup(s, i, sub.Options)
	case "toggle":
		handleCommissionToggle(s, i)
	case "addservice":
		handleCommissionAddService(s, i, sub.Options)
	case "removeservice":
		handleCommissionRemoveService(s, i, sub.Options)
	case "panel":
		handleCommissionPanel(s, i)
	case "list":
		handleCommissionList(s, i)
	case "config":
		handleCommissionConfig(s, i)
	case "setcategory":
		handleCommissionSetCategory(s, i, sub.Options)
	case "setlogchannel":
		handleCommissionSetLogChannel(s, i, sub.Options)
	}
}


func handleCommissionSetup(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	om := subOptMap(opts)
	gs := storage.GetGuild(i.GuildID)

	gs.Lock()
	gs.CommissionsRuntime.PanelChannelOverride = om["channel"].ChannelValue(s).ID
	gs.CommissionsRuntime.PayPalEmail = om["paypal-email"].StringValue()
	if v, ok := om["paypal-me"]; ok {
		gs.CommissionsRuntime.PayPalMeUser = strings.TrimPrefix(v.StringValue(), "paypal.me/")
	}
	if v, ok := om["category"]; ok {
		gs.CommissionsRuntime.DiscordCategoryOverride = v.ChannelValue(s).ID
	}
	if v, ok := om["log-channel"]; ok {
		gs.CommissionsRuntime.LogChannelOverride = v.ChannelValue(s).ID
	}
	if v, ok := om["staff-roles"]; ok {
		gs.CommissionsRuntime.StaffRolesOverride = v.StringValue()
	}
	gs.CommissionsRuntime.Enabled = true
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }

	respond(s, i, "✅ Commissions system configured and **enabled**. Use `/commission addservice` to add service types, then `/commission panel` to post the panel.", true)
}


func handleCommissionToggle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	gs.CommissionsRuntime.Enabled = !gs.CommissionsRuntime.Enabled
	enabled := gs.CommissionsRuntime.Enabled
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }

	if enabled {
		respond(s, i, "✅ Commissions are now **open**. The panel button will allow new orders.", true)
	} else {
		respond(s, i, "🔒 Commissions are now **closed**. The panel button will reject new orders.", true)
	}
}


func handleCommissionAddService(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	om := subOptMap(opts)
	gs := storage.GetGuild(i.GuildID)

	svc := config.CommissionService{
		ID:          om["id"].StringValue(),
		Name:        om["name"].StringValue(),
		Emoji:       om["emoji"].StringValue(),
		Description: om["description"].StringValue(),
	}
	if v, ok := om["starting-price"]; ok {
		svc.StartingPrice = v.StringValue()
	}

	gs.Lock()
	replaced := false
	for idx, existing := range gs.CommissionsRuntime.Services {
		if existing.ID == svc.ID {
			gs.CommissionsRuntime.Services[idx] = svc
			replaced = true
			break
		}
	}
	if !replaced {
		gs.CommissionsRuntime.Services = append(gs.CommissionsRuntime.Services, svc)
	}
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }

	verb := "added"
	if replaced {
		verb = "updated"
	}
	respond(s, i, fmt.Sprintf("✅ Service %s **%s** %s.", svc.Emoji, svc.Name, verb), true)
}

func handleCommissionRemoveService(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	om := subOptMap(opts)
	id := om["id"].StringValue()
	gs := storage.GetGuild(i.GuildID)

	gs.Lock()
	found := false
	svcs := gs.CommissionsRuntime.Services
	for idx, svc := range svcs {
		if svc.ID == id {
			gs.CommissionsRuntime.Services = append(svcs[:idx], svcs[idx+1:]...)
			found = true
			break
		}
	}
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }

	if !found {
		respond(s, i, fmt.Sprintf("❌ No runtime service with ID `%s` found. Config-file services cannot be removed at runtime.", id), true)
		return
	}
	respond(s, i, fmt.Sprintf("🗑️ Service `%s` removed.", id), true)
}


func handleCommissionPanel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	cfg := getCfg()
	gs := storage.GetGuild(i.GuildID)

	panelCh := config.EffectiveCommissionPanelChannel(cfg, gs)
	if panelCh == "" {
		respond(s, i, "❌ No panel channel configured. Run `/commission setup` first.", true)
		return
	}

	services := config.MergedCommissionServices(cfg, gs)

	gs.Lock()
	enabled := gs.CommissionsRuntime.Enabled
	oldMsgID := gs.CommissionsRuntime.PanelMessageID
	gs.Unlock()

	embed := buildCommissionPanelEmbed(gs, services, enabled)

	statusLabel := "Order Here"
	statusStyle := discordgo.SuccessButton
	if !enabled {
		statusLabel = "Commissions Closed"
		statusStyle = discordgo.DangerButton
	}

	msg, err := s.ChannelMessageSendComplex(panelCh, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    statusLabel,
						Style:    statusStyle,
						CustomID: "commission_order",
						Emoji:    &discordgo.ComponentEmoji{Name: "📋"},
					},
				},
			},
		},
	})
	if err != nil {
		respond(s, i, fmt.Sprintf("❌ Failed to send panel: %s", err.Error()), true)
		return
	}

	gs.Lock()
	gs.CommissionsRuntime.PanelMessageID = msg.ID
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }

	if oldMsgID != "" {
		_ = s.ChannelMessageDelete(panelCh, oldMsgID)
	}

	respond(s, i, "✅ Commissions panel posted.", true)
}

func buildCommissionPanelEmbed(gs *config.GuildState, services []config.CommissionService, enabled bool) *discordgo.MessageEmbed {
	cfg := getCfg()

	statusLine := "🟢 **Status: Open** — Accepting new orders!"
	if !enabled {
		statusLine = "🔴 **Status: Closed** — Not accepting orders at this time."
	}

	var desc strings.Builder
	desc.WriteString(statusLine + "\n\n")

	if len(services) == 0 {
		desc.WriteString("*No services listed yet.*\n")
	} else {
		desc.WriteString("**Available Services**\n")
		desc.WriteString("━━━━━━━━━━━━━━━━━━━━━━\n\n")
		for _, svc := range services {
			desc.WriteString(fmt.Sprintf("%s **%s**\n", svc.Emoji, svc.Name))
			desc.WriteString(fmt.Sprintf("┃ %s\n", svc.Description))
			if svc.StartingPrice != "" {
				desc.WriteString(fmt.Sprintf("┃ 💰 %s\n", svc.StartingPrice))
			}
			desc.WriteString("\n")
		}
		desc.WriteString("━━━━━━━━━━━━━━━━━━━━━━\n\n")
	}

	desc.WriteString("> 📬 Click **Order Here** to open a private commission ticket.\n")
	desc.WriteString("> You will be asked to fill out a short order form.")

	embed := &discordgo.MessageEmbed{
		Title:       "📋 Commissions",
		Description: desc.String(),
		Color:       0x5865F2,
	}
	if footer := payments.FooterText(cfg, gs); footer != "" {
		embed.Footer = &discordgo.MessageEmbedFooter{Text: footer}
	}
	return embed
}


func handleCommissionList(s *discordgo.Session, i *discordgo.InteractionCreate) {
	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	count := len(gs.CommissionsRuntime.OpenCommissions)
	var sb strings.Builder
	if count > 0 {
		sb.WriteString(fmt.Sprintf("**Open Commissions** (%d):\n", count))
		for _, ct := range gs.CommissionsRuntime.OpenCommissions {
			sb.WriteString(fmt.Sprintf("• <#%s> — #%d by <@%s> [%s]\n", ct.ChannelID, ct.Number, ct.UserID, ct.ServiceName))
		}
	}
	gs.Unlock()

	if count == 0 {
		respond(s, i, "📭 No open commissions.", true)
		return
	}
	respond(s, i, sb.String(), true)
}


func handleCommissionConfig(s *discordgo.Session, i *discordgo.InteractionCreate) {
	cfg := getCfg()
	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	cr := gs.CommissionsRuntime
	gs.Unlock()

	services := config.MergedCommissionServices(cfg, gs)

	var sb strings.Builder
	sb.WriteString("**Commissions System Configuration**\n\n")
	sb.WriteString(fmt.Sprintf("Enabled: `%v`\n", cr.Enabled))
	sb.WriteString(fmt.Sprintf("Panel Channel: `%s`\n", cr.PanelChannelOverride))
	sb.WriteString(fmt.Sprintf("Log Channel: `%s`\n", cr.LogChannelOverride))
	sb.WriteString(fmt.Sprintf("Staff Roles: `%s`\n", cr.StaffRolesOverride))
	sb.WriteString(fmt.Sprintf("Discord Category: `%s`\n", cr.DiscordCategoryOverride))
	sb.WriteString(fmt.Sprintf("PayPal Email: `%s`\n", cr.PayPalEmail))
	sb.WriteString(fmt.Sprintf("PayPal.me User: `%s`\n", cr.PayPalMeUser))
	sb.WriteString(fmt.Sprintf("Open Commissions: `%d`\n", len(cr.OpenCommissions)))
	sb.WriteString(fmt.Sprintf("Total Invoices: `%d`\n\n", len(cr.Invoices)))
	sb.WriteString("__Services:__\n")
	for _, svc := range services {
		price := ""
		if svc.StartingPrice != "" {
			price = fmt.Sprintf(" — %s", svc.StartingPrice)
		}
		sb.WriteString(fmt.Sprintf("• %s **%s** (`%s`)%s\n", svc.Emoji, svc.Name, svc.ID, price))
	}
	if len(services) == 0 {
		sb.WriteString("*No services configured.*\n")
	}

	respond(s, i, sb.String(), true)
}


func handleCommissionSetCategory(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	om := subOptMap(opts)
	id := strings.TrimSpace(om["id"].StringValue())
	if !config.IsSnowflake(id) {
		respond(s, i, "❌ That doesn't look like a valid Discord snowflake ID. Right-click the category and choose **Copy ID** (Developer Mode must be on).", true)
		return
	}
	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	gs.CommissionsRuntime.DiscordCategoryOverride = id
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }
	respond(s, i, fmt.Sprintf("✅ Commission category set to `%s`. New commission channels will be created inside that category.", id), true)
}

func handleCommissionSetLogChannel(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	om := subOptMap(opts)
	id := strings.TrimSpace(om["id"].StringValue())
	if !config.IsSnowflake(id) {
		respond(s, i, "❌ That doesn't look like a valid Discord snowflake ID. Right-click the channel and choose **Copy ID**.", true)
		return
	}
	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	gs.CommissionsRuntime.LogChannelOverride = id
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }
	respond(s, i, fmt.Sprintf("✅ Commission log channel set to <#%s>.", id), true)
}


func handleInvoiceCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	sub := i.ApplicationCommandData().Options[0]
	switch sub.Name {
	case "create":
		handleInvoiceCreate(s, i, sub.Options)
	case "list":
		handleInvoiceList(s, i)
	}
}


func handleInvoiceCreate(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	om := subOptMap(opts)
	cfg := getCfg()
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

	var gatewayButtons []discordgo.MessageComponent
	if payments.Svc != nil {
		for _, btn := range payments.Svc.CreateLinks(&inv) {
			gatewayButtons = append(gatewayButtons, discordgo.Button{
				Label: btn.Label,
				Style: discordgo.LinkButton,
				URL:   btn.URL,
				Emoji: &discordgo.ComponentEmoji{Name: btn.Emoji},
			})
		}
	}

	if len(gatewayButtons) == 0 && paypalMe != "" {
		paypalURL := fmt.Sprintf("https://paypal.me/%s/%.2f%s", paypalMe, amount, currency)
		gatewayButtons = append(gatewayButtons, discordgo.Button{
			Label: fmt.Sprintf("Pay %.2f %s via PayPal", amount, currency),
			Style: discordgo.LinkButton,
			URL:   paypalURL,
			Emoji: &discordgo.ComponentEmoji{Name: "💳"},
		})
	}

	gs.Lock()
	gs.CommissionsRuntime.Invoices = append(gs.CommissionsRuntime.Invoices, inv)
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }

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

	send := &discordgo.MessageSend{
		Content: fmt.Sprintf("<@%s>", client.ID),
		Embeds:  []*discordgo.MessageEmbed{embed},
	}
	if len(gatewayButtons) > 0 {
		for start := 0; start < len(gatewayButtons); start += 5 {
			end := start + 5
			if end > len(gatewayButtons) {
				end = len(gatewayButtons)
			}
			send.Components = append(send.Components, discordgo.ActionsRow{
				Components: gatewayButtons[start:end],
			})
		}
	}

	_, err := s.ChannelMessageSendComplex(i.ChannelID, send)
	if err != nil {
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


func handleCommissionOrder(s *discordgo.Session, i *discordgo.InteractionCreate) {
	cfg := getCfg()
	gs := storage.GetGuild(i.GuildID)

	gs.Lock()
	guildEnabled := gs.CommissionsRuntime.Enabled
	guildConfigured := gs.CommissionsRuntime.PanelChannelOverride != "" ||
		gs.CommissionsRuntime.StaffRolesOverride != "" ||
		len(gs.CommissionsRuntime.OpenCommissions) > 0
	gs.Unlock()

	var enabled bool
	if guildConfigured {
		enabled = guildEnabled
	} else {
		enabled = cfg.Commissions.Enabled
	}

	if !enabled {
		respond(s, i, "🔒 Commissions are currently **closed**. Please check back later!", true)
		return
	}

	services := config.MergedCommissionServices(cfg, gs)
	if len(services) == 0 {
		respond(s, i, "⚠️ No services are available at the moment. Please check back later!", true)
		return
	}

	opts := make([]discordgo.SelectMenuOption, 0, len(services))
	for _, svc := range services {
		opts = append(opts, discordgo.SelectMenuOption{
			Label:       svc.Name,
			Value:       svc.ID,
			Description: svc.Description,
			Emoji:       parseComponentEmoji(svc.Emoji),
		})
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "**What service are you interested in?**\nSelect a service type below to continue your order.",
			Flags:   discordgo.MessageFlagsEphemeral,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							MenuType:    discordgo.StringSelectMenu,
							CustomID:    "commission_service_select",
							Placeholder: "Choose a service...",
							Options:     opts,
						},
					},
				},
			},
		},
	}); err != nil {
		slog.Warn("Commission order InteractionRespond error", "error", err)
	}
}


func handleCommissionServiceSelect(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if len(data.Values) == 0 {
		return
	}
	serviceID := data.Values[0]

	cfg := getCfg()
	gs := storage.GetGuild(i.GuildID)
	services := config.MergedCommissionServices(cfg, gs)

	var svc *config.CommissionService
	for idx := range services {
		if services[idx].ID == serviceID {
			svc = &services[idx]
			break
		}
	}
	if svc == nil {
		respond(s, i, "❌ That service no longer exists. Please try again.", true)
		return
	}

	modalTitle := fmt.Sprintf("Order — %s %s", svc.Emoji, svc.Name)
	if len(modalTitle) > 45 {
		modalTitle = modalTitle[:45]
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "commission_form:" + serviceID,
			Title:    modalTitle,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "details",
						Label:       "What do you want commissioned?",
						Style:       discordgo.TextInputParagraph,
						Required:    true,
						Placeholder: "Describe exactly what you need. The more detail the better!",
						MinLength:   20,
						MaxLength:   1000,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "budget",
						Label:       "Your Budget",
						Style:       discordgo.TextInputShort,
						Required:    true,
						Placeholder: "e.g. $50–$100 or open to quote",
						MaxLength:   100,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "timeline",
						Label:       "Timeline / Deadline",
						Style:       discordgo.TextInputShort,
						Required:    true,
						Placeholder: "e.g. 2 weeks, ASAP, by Dec 1",
						MaxLength:   100,
					},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{
						CustomID:    "notes",
						Label:       "Additional Notes (optional)",
						Style:       discordgo.TextInputParagraph,
						Required:    false,
						Placeholder: "Anything else we should know? References, examples, etc.",
						MaxLength:   500,
					},
				}},
			},
		},
	})
}


func handleCommissionFormSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	serviceID := strings.TrimPrefix(data.CustomID, "commission_form:")

	fields := modalTextValues(data.Components)
	details := strings.TrimSpace(fields["details"])
	budget := strings.TrimSpace(fields["budget"])
	timeline := strings.TrimSpace(fields["timeline"])
	notes := strings.TrimSpace(fields["notes"])

	if details == "" || budget == "" || timeline == "" {
		slog.Warn("Commission modal submitted with empty required fields", "user", i.Member.User.ID, "service", serviceID, "details", details, "budget", budget, "timeline", timeline)
	}

	cfg := getCfg()
	gs := storage.GetGuild(i.GuildID)
	services := config.MergedCommissionServices(cfg, gs)

	var svc *config.CommissionService
	for idx := range services {
		if services[idx].ID == serviceID {
			svc = &services[idx]
			break
		}
	}
	serviceName := serviceID
	serviceEmoji := ""
	if svc != nil {
		serviceName = svc.Name
		serviceEmoji = svc.Emoji
	}

	createCommissionChannel(s, i, serviceID, serviceName, serviceEmoji, details, budget, timeline, notes)
}


func createCommissionChannel(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	serviceID, serviceName, serviceEmoji,
	details, budget, timeline, notes string,
) {
	cfg := getCfg()
	gs := storage.GetGuild(i.GuildID)
	userID := i.Member.User.ID

	gs.Lock()
	gs.CommissionsRuntime.CommissionCounter++
	num := gs.CommissionsRuntime.CommissionCounter
	gs.Unlock()

	channelName := fmt.Sprintf("commission-%04d", num)
	discordCat := config.EffectiveCommissionCategory(cfg, gs)
	staffRoles := config.EffectiveCommissionStaffRoles(cfg, gs)

	overwrites := []*discordgo.PermissionOverwrite{
		{ID: i.GuildID, Type: discordgo.PermissionOverwriteTypeRole, Deny: discordgo.PermissionViewChannel},
		{
			ID:    userID,
			Type:  discordgo.PermissionOverwriteTypeMember,
			Allow: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionAttachFiles | discordgo.PermissionReadMessageHistory,
		},
	}

	ch, err := s.GuildChannelCreateComplex(i.GuildID, discordgo.GuildChannelCreateData{
		Name:                 channelName,
		Type:                 discordgo.ChannelTypeGuildText,
		ParentID:             discordCat,
		PermissionOverwrites: overwrites,
	})
	if err != nil {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ Failed to create commission channel: %s", err.Error()),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	ct := config.CommissionTicket{
		ChannelID:            ch.ID,
		UserID:               userID,
		ServiceID:            serviceID,
		ServiceName:          serviceName,
		Details:              details,
		Budget:               budget,
		Timeline:             timeline,
		Notes:                notes,
		Number:               num,
		CreatedAt:            time.Now().Format(time.RFC3339),
		ClientThreadMessages: make(map[string]string),
		Quotes:               make(map[string]config.CommissionQuote),
	}

	gs.Lock()
	gs.CommissionsRuntime.OpenCommissions[ch.ID] = ct
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }

	serviceDisplay := serviceName
	if serviceEmoji != "" {
		serviceDisplay = serviceEmoji + " " + serviceName
	}

	notesField := "*None provided*"
	if notes != "" {
		notesField = notes
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("📋 Commission #%04d", num),
		Color: 0x5865F2,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Client", Value: fmt.Sprintf("<@%s>", userID), Inline: true},
			{Name: "Service", Value: serviceDisplay, Inline: true},
			{Name: "Budget", Value: budget, Inline: true},
			{Name: "Timeline", Value: timeline, Inline: true},
			{Name: "Order Details", Value: details, Inline: false},
			{Name: "Additional Notes", Value: notesField, Inline: false},
		},
		Footer:    &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Commission opened • %s", time.Now().Format("Jan 2, 2006 15:04"))},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	pingContent := fmt.Sprintf("<@%s>", userID)
	for _, roleID := range staffRoles {
		pingContent += fmt.Sprintf(" <@&%s>", roleID)
	}

	detailsMsg, err := s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{
		Content: pingContent,
		Embeds:  []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label:    "Issue Invoice",
						Style:    discordgo.SuccessButton,
						CustomID: "commission_invoice_btn:" + ch.ID,
						Emoji:    &discordgo.ComponentEmoji{Name: "🧾"},
					},
					discordgo.Button{
						Label:    "Close Commission",
						Style:    discordgo.DangerButton,
						CustomID: "commission_close_btn",
						Emoji:    &discordgo.ComponentEmoji{Name: "🔒"},
					},
				},
			},
		},
	})
	if err != nil {
		slog.Warn("Commission failed to send details message", "channel", ch.ID, "error", err)
	} else {
		if pinErr := s.ChannelMessagePin(ch.ID, detailsMsg.ID); pinErr != nil {
			slog.Warn("Commission failed to pin details message", "channel", ch.ID, "error", pinErr)
		}
	}

	logCh := config.EffectiveCommissionLogChannel(cfg, gs)
	if logCh != "" {
		logEmbed := &discordgo.MessageEmbed{
			Title: fmt.Sprintf("New Commission for %s", serviceDisplay),
			Color: 0x57F287,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Client", Value: fmt.Sprintf("<@%s>", userID), Inline: true},
				{Name: "Service", Value: serviceDisplay, Inline: true},
				{Name: "Access", Value: "No freelancer selected yet", Inline: true},
				{Name: "Budget", Value: safeEmbedValue(budget), Inline: true},
				{Name: "Timeframe", Value: safeEmbedValue(timeline), Inline: true},
				{Name: "Project Description", Value: safeEmbedValue(details), Inline: false},
				{Name: "Additional Notes", Value: safeEmbedValue(notesField), Inline: false},
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
		logPing := ""
		for _, roleID := range staffRoles {
			logPing += fmt.Sprintf("<@&%s> ", roleID)
		}
		logMsg, err := s.ChannelMessageSendComplex(logCh, &discordgo.MessageSend{
			Content:    strings.TrimSpace(logPing),
			Embeds:     []*discordgo.MessageEmbed{logEmbed},
			Components: commissionLogComponents(i.GuildID, ch.ID, "", true, false),
		})
		if err != nil {
			slog.Warn("Commission failed to post log message", "channel", ch.ID, "error", err)
		} else {
			ct.LogChannelID = logCh
			ct.LogMessageID = logMsg.ID
			thread, threadErr := s.MessageThreadStartComplex(logCh, logMsg.ID, &discordgo.ThreadStart{
				Name:                fmt.Sprintf("commission-%04d-discussion", num),
				AutoArchiveDuration: 10080,
			})
			if threadErr != nil {
				slog.Warn("Commission failed to create discussion thread", "channel", ch.ID, "error", threadErr)
			} else {
				ct.DiscussionThreadID = thread.ID
				_, _ = s.ChannelMessageSend(thread.ID, formatCommissionInitialThreadMessage(&ct))
				components := commissionLogComponents(i.GuildID, ch.ID, thread.ID, true, false)
				_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{ID: logMsg.ID, Channel: logCh, Components: &components})
			}
			gs.Lock()
			gs.CommissionsRuntime.OpenCommissions[ch.ID] = ct
			gs.Unlock()
			if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }
		}
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("✅ Your commission ticket has been opened: <#%s>\n\nOur team will review your order and get back to you shortly!", ch.ID),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
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

	cfg := getCfg()

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

	var gatewayButtons []discordgo.MessageComponent
	if payments.Svc != nil {
		for _, btn := range payments.Svc.CreateLinks(&inv) {
			gatewayButtons = append(gatewayButtons, discordgo.Button{
				Label: btn.Label,
				Style: discordgo.LinkButton,
				URL:   btn.URL,
				Emoji: &discordgo.ComponentEmoji{Name: btn.Emoji},
			})
		}
	}
	if len(gatewayButtons) == 0 && paypalMe != "" {
		paypalURL := fmt.Sprintf("https://paypal.me/%s/%.2f%s", paypalMe, amount, currency)
		gatewayButtons = append(gatewayButtons, discordgo.Button{
			Label: fmt.Sprintf("Pay %.2f %s via PayPal", amount, currency),
			Style: discordgo.LinkButton,
			URL:   paypalURL,
			Emoji: &discordgo.ComponentEmoji{Name: "💳"},
		})
	}

	gs.Lock()
	gs.CommissionsRuntime.Invoices = append(gs.CommissionsRuntime.Invoices, inv)
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }

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

	send := &discordgo.MessageSend{
		Content: fmt.Sprintf("<@%s>", ct.UserID),
		Embeds:  []*discordgo.MessageEmbed{embed},
	}
	if len(gatewayButtons) > 0 {
		for start := 0; start < len(gatewayButtons); start += 5 {
			end := start + 5
			if end > len(gatewayButtons) {
				end = len(gatewayButtons)
			}
			send.Components = append(send.Components, discordgo.ActionsRow{
				Components: gatewayButtons[start:end],
			})
		}
	}

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


func handleCommissionCloseButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	_, ok := gs.CommissionsRuntime.OpenCommissions[i.ChannelID]
	gs.Unlock()
	if !ok {
		respond(s, i, "❌ This is not an active commission channel.", true)
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Are you sure you want to close this commission? The channel will be deleted.",
			Flags:   discordgo.MessageFlagsEphemeral,
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.Button{Label: "Yes, close it", Style: discordgo.DangerButton, CustomID: "commission_close_confirm"},
						discordgo.Button{Label: "Cancel", Style: discordgo.SecondaryButton, CustomID: "commission_close_cancel"},
					},
				},
			},
		},
	})
}

func handleCommissionCloseConfirm(s *discordgo.Session, i *discordgo.InteractionCreate) {
	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	ct, ok := gs.CommissionsRuntime.OpenCommissions[i.ChannelID]
	gs.Unlock()
	if !ok {
		respond(s, i, "❌ Commission not found.", true)
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: "🔒 Closing commission..."},
	})

	go closeCommissionChannel(s, i.GuildID, i.ChannelID, i.Member.User, &ct, gs)
}

func handleCommissionCloseCancel(s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "Cancelled. Commission is still open.",
			Components: []discordgo.MessageComponent{},
		},
	})
}

func closeCommissionChannel(s *discordgo.Session, guildID, channelID string, closedBy *discordgo.User, ct *config.CommissionTicket, gs *config.GuildState) {
	cfg := getCfg()
	transcript := generateTranscript(s, channelID)

	logCh := config.EffectiveCommissionLogChannel(cfg, gs)
	if logCh != "" {
		embed := &discordgo.MessageEmbed{
			Title: fmt.Sprintf("Commission #%04d Closed", ct.Number),
			Color: 0xED4245,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Client", Value: fmt.Sprintf("<@%s>", ct.UserID), Inline: true},
				{Name: "Closed By", Value: fmt.Sprintf("<@%s>", closedBy.ID), Inline: true},
				{Name: "Service", Value: ct.ServiceName, Inline: true},
				{Name: "Budget", Value: ct.Budget, Inline: true},
				{Name: "Timeline", Value: ct.Timeline, Inline: true},
				{Name: "Opened At", Value: ct.CreatedAt, Inline: true},
			},
			Timestamp: time.Now().Format(time.RFC3339),
		}
		_, _ = s.ChannelMessageSendComplex(logCh, &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{embed},
			Files: []*discordgo.File{
				{
					Name:        fmt.Sprintf("commission-%04d-transcript.txt", ct.Number),
					ContentType: "text/plain",
					Reader:      strings.NewReader(transcript),
				},
			},
		})
	}

	gs.Lock()
	delete(gs.CommissionsRuntime.OpenCommissions, channelID)
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }

	time.Sleep(3 * time.Second)
	_, _ = s.ChannelDelete(channelID)
}

func handleCommissionMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}
	gs, ct, ok, fromTicket := findCommissionTicketForMessage(m)
	if !ok {
		return
	}
	if fromTicket {
		handleCommissionClientMessage(s, gs, &ct, m)
		return
	}
	handleCommissionThreadReply(s, &ct, m)
}

func findCommissionTicketForMessage(m *discordgo.MessageCreate) (*config.GuildState, config.CommissionTicket, bool, bool) {
	if m.GuildID != "" {
		gs := storage.GetGuild(m.GuildID)
		if ct, ok, fromTicket := findCommissionTicketInGuild(gs, m.ChannelID); ok {
			return gs, ct, true, fromTicket
		}
	}
	for _, gs := range storage.GetAllGuildStates() {
		if ct, ok, fromTicket := findCommissionTicketInGuild(gs, m.ChannelID); ok {
			return gs, ct, true, fromTicket
		}
	}
	return nil, config.CommissionTicket{}, false, false
}

func findCommissionTicketInGuild(gs *config.GuildState, channelID string) (config.CommissionTicket, bool, bool) {
	gs.Lock()
	defer gs.Unlock()
	for ticketChannelID, ticket := range gs.CommissionsRuntime.OpenCommissions {
		if channelID == ticketChannelID {
			return ticket, true, true
		}
		if ticket.DiscussionThreadID != "" && channelID == ticket.DiscussionThreadID {
			return ticket, true, false
		}
	}
	return config.CommissionTicket{}, false, false
}

func handleCommissionClientMessage(s *discordgo.Session, gs *config.GuildState, ct *config.CommissionTicket, m *discordgo.MessageCreate) {
	if m.Author.ID != ct.UserID {
		return
	}
	if ct.DiscussionThreadID == "" {
		slog.Warn("Commission client message received but discussion thread not set", "ticket", ct.ChannelID)
		return
	}
	content := formatCommissionClientMirror(m)
	threadMsg, err := s.ChannelMessageSendComplex(ct.DiscussionThreadID, &discordgo.MessageSend{
		Content:         content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
	if err != nil {
		slog.Warn("Commission failed to mirror client message to thread", "message", m.ID, "thread", ct.DiscussionThreadID, "error", err)
		return
	}

	gs.Lock()
	stored := gs.CommissionsRuntime.OpenCommissions[ct.ChannelID]
	ensureCommissionTicketRuntime(&stored)
	stored.ClientThreadMessages[m.ID] = threadMsg.ID
	gs.CommissionsRuntime.OpenCommissions[ct.ChannelID] = stored
	gs.Unlock()
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }
}

func handleCommissionThreadReply(s *discordgo.Session, ct *config.CommissionTicket, m *discordgo.MessageCreate) {
	content := formatCommissionFreelancerReply(m)
	_, err := s.ChannelMessageSendComplex(ct.ChannelID, &discordgo.MessageSend{
		Content:         content,
		AllowedMentions: &discordgo.MessageAllowedMentions{},
	})
	if err != nil {
		slog.Warn("Commission failed to forward thread message to ticket", "message", m.ID, "ticket", ct.ChannelID, "error", err)
	}
}

func commissionThreadReferenceTargetsClient(s *discordgo.Session, ct *config.CommissionTicket, threadID, referencedID string) bool {
	if referencedID == ct.LogMessageID {
		return true
	}
	for _, threadMsgID := range ct.ClientThreadMessages {
		if threadMsgID == referencedID {
			return true
		}
	}
	ref, err := s.ChannelMessage(threadID, referencedID)
	if err != nil || ref == nil || ref.Author == nil {
		return false
	}
	if s.State != nil && s.State.User != nil && ref.Author.ID == s.State.User.ID {
		content := strings.TrimSpace(ref.Content)
		return strings.HasPrefix(content, "Client ") || strings.HasPrefix(content, "Commission brief") || strings.Contains(content, "Project Description")
	}
	return false
}
func formatCommissionClientMirror(m *discordgo.MessageCreate) string {
	body := strings.TrimSpace(m.Content)
	if body == "" {
		body = "*(no text content)*"
	}
	if len(m.Attachments) > 0 {
		var urls []string
		for _, a := range m.Attachments {
			urls = append(urls, a.URL)
		}
		body += "\n\nAttachments:\n" + strings.Join(urls, "\n")
	}
	return truncateMessage(fmt.Sprintf("Client <@%s> wrote:\n%s", m.Author.ID, body), 1900)
}

func formatCommissionFreelancerReply(m *discordgo.MessageCreate) string {
	body := strings.TrimSpace(m.Content)
	if body == "" {
		body = "*(no text content)*"
	}
	if len(m.Attachments) > 0 {
		var urls []string
		for _, a := range m.Attachments {
			urls = append(urls, a.URL)
		}
		body += "\n\nAttachments:\n" + strings.Join(urls, "\n")
	}
	return truncateMessage(fmt.Sprintf("Freelancer %s: %s", m.Author.Username, body), 1900)
}

func handleCommissionQuoteButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID := strings.TrimPrefix(i.MessageComponentData().CustomID, "commission_quote_btn:")
	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	ct, ok := gs.CommissionsRuntime.OpenCommissions[channelID]
	gs.Unlock()
	if !ok {
		respond(s, i, "Could not find this commission ticket.", true)
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "commission_quote_modal:" + channelID,
			Title:    fmt.Sprintf("Quote Commission #%04d", ct.Number),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{CustomID: "amount", Label: "Quote amount", Style: discordgo.TextInputShort, Required: true, Placeholder: "50.00", MaxLength: 20}}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{CustomID: "currency", Label: "Currency", Style: discordgo.TextInputShort, Required: false, Placeholder: "EUR", MaxLength: 5}}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{CustomID: "timeline", Label: "Delivery timeline", Style: discordgo.TextInputShort, Required: true, Placeholder: "e.g. 3 days", MaxLength: 100}}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{CustomID: "message", Label: "Message to client", Style: discordgo.TextInputParagraph, Required: true, Placeholder: "Explain what is included in your quote.", MaxLength: 800}}},
			},
		},
	})
}

func handleCommissionQuoteModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ModalSubmitData()
	channelID := strings.TrimPrefix(data.CustomID, "commission_quote_modal:")
	fields := modalTextValues(data.Components)

	amountStr := strings.ReplaceAll(strings.TrimSpace(fields["amount"]), ",", ".")
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		respond(s, i, "Invalid quote amount. Use a number like `50.00`.", true)
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(fields["currency"]))
	if currency == "" {
		currency = "EUR"
	}
	quoteID := fmt.Sprintf("q%d", time.Now().UnixNano())
	quote := config.CommissionQuote{
		ID:           quoteID,
		FreelancerID: i.Member.User.ID,
		Amount:       amount,
		Currency:     currency,
		Timeline:     strings.TrimSpace(fields["timeline"]),
		Message:      strings.TrimSpace(fields["message"]),
		Status:       "pending",
		CreatedAt:    time.Now().Format(time.RFC3339),
	}

	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	ct, ok := gs.CommissionsRuntime.OpenCommissions[channelID]
	if ok {
		ensureCommissionTicketRuntime(&ct)
		ct.Quotes[quoteID] = quote
		gs.CommissionsRuntime.OpenCommissions[channelID] = ct
	}
	gs.Unlock()
	if !ok {
		respond(s, i, "Could not find this commission ticket.", true)
		return
	}
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }

	embed := &discordgo.MessageEmbed{
		Title: "New Freelancer Quote",
		Color: 0xF0A500,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Freelancer", Value: fmt.Sprintf("<@%s>", quote.FreelancerID), Inline: true},
			{Name: "Amount", Value: fmt.Sprintf("%.2f %s", quote.Amount, quote.Currency), Inline: true},
			{Name: "Timeline", Value: safeEmbedValue(quote.Timeline), Inline: true},
			{Name: "Message", Value: safeEmbedValue(quote.Message), Inline: false},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
	_, sendErr := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: fmt.Sprintf("<@%s>", ct.UserID),
		Embeds:  []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{Label: "Accept Quote", Style: discordgo.SuccessButton, CustomID: "commission_quote_accept:" + channelID + ":" + quoteID},
			discordgo.Button{Label: "Decline Quote", Style: discordgo.DangerButton, CustomID: "commission_quote_decline:" + channelID + ":" + quoteID},
		}}},
	})
	if sendErr != nil {
		respond(s, i, fmt.Sprintf("Quote saved, but failed to post in ticket: `%s`", sendErr.Error()), true)
		return
	}
	respond(s, i, fmt.Sprintf("Quote sent to <@%s> in <#%s>.", ct.UserID, channelID), true)
}

func handleCommissionQuoteAccept(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID, quoteID, ok := parseCommissionQuoteComponent(i.MessageComponentData().CustomID, "commission_quote_accept:")
	if !ok {
		respond(s, i, "Invalid quote action.", true)
		return
	}

	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	ct, exists := gs.CommissionsRuntime.OpenCommissions[channelID]
	var quote config.CommissionQuote
	quoteExists := false
	if exists {
		ensureCommissionTicketRuntime(&ct)
		quote, quoteExists = ct.Quotes[quoteID]
	}
	if exists && quoteExists && i.Member.User.ID == ct.UserID {
		for id, existing := range ct.Quotes {
			if existing.Status == "accepted" {
				existing.Status = "superseded"
				ct.Quotes[id] = existing
			}
		}
		quote.Status = "accepted"
		ct.Quotes[quoteID] = quote
		ct.AcceptedFreelancerID = quote.FreelancerID
		gs.CommissionsRuntime.OpenCommissions[channelID] = ct
	}
	gs.Unlock()

	if !exists {
		respond(s, i, "Could not find this commission ticket.", true)
		return
	}
	if i.Member.User.ID != ct.UserID {
		respond(s, i, "Only the client can accept this quote.", true)
		return
	}
	if !quoteExists {
		respond(s, i, "Could not find that quote.", true)
		return
	}

	allow := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionAttachFiles | discordgo.PermissionReadMessageHistory)
	if err := s.ChannelPermissionSet(channelID, quote.FreelancerID, discordgo.PermissionOverwriteTypeMember, allow, 0); err != nil {
		respond(s, i, fmt.Sprintf("Quote accepted, but I could not open the ticket for the freelancer: `%s`", err.Error()), true)
		return
	}

	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }
	_, _ = s.ChannelMessageSend(channelID, fmt.Sprintf("Quote accepted by <@%s>. <@%s> now has access to this ticket.", ct.UserID, quote.FreelancerID))
	if ct.DiscussionThreadID != "" {
		_, _ = s.ChannelMessageSend(ct.DiscussionThreadID, fmt.Sprintf("Quote `%s` was accepted by <@%s>. <@%s> now has access to <#%s>.", quoteID, ct.UserID, quote.FreelancerID, channelID))
	}

	if ct.LogMessageID != "" && ct.LogChannelID != "" {
		components := commissionLogComponents(i.GuildID, channelID, ct.DiscussionThreadID, false, true)
		if components != nil {
			_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				ID:         ct.LogMessageID,
				Channel:    ct.LogChannelID,
				Components: &components,
			})
		}
	}

	sendCommissionAcceptedDM(s, quote.FreelancerID, &ct, &quote)
	respond(s, i, "Quote accepted. The freelancer can now access this ticket.", true)
}

func sendCommissionAcceptedDM(s *discordgo.Session, freelancerID string, ct *config.CommissionTicket, quote *config.CommissionQuote) {
	dm, err := s.UserChannelCreate(freelancerID)
	if err != nil {
		slog.Warn("Commission failed to create DM for accepted freelancer", "freelancer", freelancerID, "error", err)
		return
	}
	msg := fmt.Sprintf("OreoManager: Your quote for Commission #%04d was accepted. You can now access the ticket: <#%s>\nAmount: %.2f %s\nTimeline: %s", ct.Number, ct.ChannelID, quote.Amount, quote.Currency, quote.Timeline)
	if _, err := s.ChannelMessageSend(dm.ID, msg); err != nil {
		slog.Warn("Commission failed to DM accepted freelancer", "freelancer", freelancerID, "error", err)
	}
}
func handleCommissionQuoteDecline(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID, quoteID, ok := parseCommissionQuoteComponent(i.MessageComponentData().CustomID, "commission_quote_decline:")
	if !ok {
		respond(s, i, "Invalid quote action.", true)
		return
	}
	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	ct, exists := gs.CommissionsRuntime.OpenCommissions[channelID]
	gs.Unlock()
	if !exists {
		respond(s, i, "Could not find this commission ticket.", true)
		return
	}
	if i.Member.User.ID != ct.UserID {
		respond(s, i, "Only the client can decline this quote.", true)
		return
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "commission_quote_decline_modal:" + channelID + ":" + quoteID,
			Title:    "Decline Quote",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{discordgo.TextInput{CustomID: "reason", Label: "Why are you declining?", Style: discordgo.TextInputParagraph, Required: true, MaxLength: 800}}},
			},
		},
	})
}

func handleCommissionQuoteDeclineModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate) {
	channelID, quoteID, ok := parseCommissionQuoteComponent(i.ModalSubmitData().CustomID, "commission_quote_decline_modal:")
	if !ok {
		respond(s, i, "Invalid quote action.", true)
		return
	}
	reason := strings.TrimSpace(modalTextValues(i.ModalSubmitData().Components)["reason"])
	gs := storage.GetGuild(i.GuildID)
	gs.Lock()
	ct, exists := gs.CommissionsRuntime.OpenCommissions[channelID]
	if exists && i.Member.User.ID == ct.UserID {
		ensureCommissionTicketRuntime(&ct)
		quote := ct.Quotes[quoteID]
		quote.Status = "declined"
		quote.Reason = reason
		ct.Quotes[quoteID] = quote
		gs.CommissionsRuntime.OpenCommissions[channelID] = ct
	}
	gs.Unlock()
	if !exists {
		respond(s, i, "Could not find this commission ticket.", true)
		return
	}
	if i.Member.User.ID != ct.UserID {
		respond(s, i, "Only the client can decline this quote.", true)
		return
	}
	if err := gs.Save(); err != nil { slog.Warn("Failed to save guild state", "error", err) }
	msg := fmt.Sprintf("Quote declined by <@%s>. Reason: %s", ct.UserID, reason)
	_, _ = s.ChannelMessageSend(channelID, msg)
	if ct.DiscussionThreadID != "" {
		_, _ = s.ChannelMessageSend(ct.DiscussionThreadID, fmt.Sprintf("Quote `%s` was declined. Reason: %s", quoteID, reason))
	}
	respond(s, i, "Quote declined and your reason was sent.", true)
}

func modalTextValues(rows []discordgo.MessageComponent) map[string]string {
	values := make(map[string]string)
	for _, row := range rows {
		var components []discordgo.MessageComponent
		switch ar := row.(type) {
		case discordgo.ActionsRow:
			components = ar.Components
		case *discordgo.ActionsRow:
			if ar != nil {
				components = ar.Components
			}
		default:
			continue
		}
		for _, comp := range components {
			switch ti := comp.(type) {
			case discordgo.TextInput:
				values[ti.CustomID] = ti.Value
			case *discordgo.TextInput:
				if ti != nil {
					values[ti.CustomID] = ti.Value
				}
			}
		}
	}
	return values
}

func mapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func parseCommissionQuoteComponent(customID, prefix string) (string, string, bool) {
	rest := strings.TrimPrefix(customID, prefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func ensureCommissionTicketRuntime(ct *config.CommissionTicket) {
	if ct.ClientThreadMessages == nil {
		ct.ClientThreadMessages = make(map[string]string)
	}
	if ct.Quotes == nil {
		ct.Quotes = make(map[string]config.CommissionQuote)
	}
}

func safeEmbedValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "*None provided*"
	}
	return truncateMessage(value, 1024)
}

func truncateMessage(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func commissionLogComponents(guildID, ticketChannelID, threadID string, allowQuote bool, showViewLink bool) []discordgo.MessageComponent {
	var buttons []discordgo.MessageComponent
	if showViewLink {
		buttons = append(buttons, discordgo.Button{Label: "View Commission", Style: discordgo.LinkButton, URL: discordChannelURL(guildID, ticketChannelID)})
	}
	if allowQuote {
		buttons = append(buttons, discordgo.Button{Label: "Send Quote", Style: discordgo.PrimaryButton, CustomID: "commission_quote_btn:" + ticketChannelID})
	}
	if threadID != "" {
		buttons = append(buttons, discordgo.Button{Label: "Open Thread", Style: discordgo.LinkButton, URL: discordChannelURL(guildID, threadID)})
	}
	if len(buttons) == 0 {
		return nil
	}
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}
}

func discordChannelURL(guildID, channelID string) string {
	return fmt.Sprintf("https://discord.com/channels/%s/%s", guildID, channelID)
}

func formatCommissionInitialThreadMessage(ct *config.CommissionTicket) string {
	parts := []string{
		fmt.Sprintf("Commission brief for #%04d", ct.Number),
		fmt.Sprintf("Client: <@%s>", ct.UserID),
		fmt.Sprintf("Service: %s", ct.ServiceName),
		fmt.Sprintf("Budget: %s", safeEmbedValue(ct.Budget)),
		fmt.Sprintf("Timeframe: %s", safeEmbedValue(ct.Timeline)),
		"Project Description:",
		safeEmbedValue(ct.Details),
	}
	if strings.TrimSpace(ct.Notes) != "" {
		parts = append(parts, "Additional Notes:", safeEmbedValue(ct.Notes))
	}
	parts = append(parts, "", "When the buyer writes in their private ticket, their message will appear here. Reply directly to a mirrored client message to send your response back to the buyer. Use Send Quote on the log message to quote the job.")
	return truncateMessage(strings.Join(parts, "\n"), 1900)
}
