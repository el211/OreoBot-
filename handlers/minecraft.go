package handlers

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"discord-bot/lang"

	"github.com/bwmarrin/discordgo"
)

type pendingLink struct {
	discordID string
	expiresAt time.Time
}

var (
	pendingLinks   = make(map[string]pendingLink)
	pendingLinksMu sync.Mutex
)

func generateLinkCode() string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	code := make([]byte, 6)
	for i := range code {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		code[i] = chars[n.Int64()]
	}
	return string(code)
}

func ConsumeLinkCode(code string) (discordID string, valid bool) {
	pendingLinksMu.Lock()
	defer pendingLinksMu.Unlock()

	p, ok := pendingLinks[code]
	if !ok {
		return "", false
	}
	if time.Now().After(p.expiresAt) {
		delete(pendingLinks, code)
		return "", false
	}
	delete(pendingLinks, code)
	return p.discordID, true
}

func StartLinkPoller(s *discordgo.Session, guildID string) {
	poll := func() {
		store := getMCStore()
		if store == nil {
			return
		}
		confirmations, err := store.PopConfirmed()
		if err != nil {
			slog.Warn("MC PopConfirmed error", "error", err)
			return
		}
		for _, c := range confirmations {
			link := MCLink{
				DiscordID: c.DiscordID,
				UUID:      c.UUID,
				Username:  c.Username,
				LinkedAt:  time.Now().Format("2006-01-02 15:04"),
			}
			if err := store.SaveLink(link); err != nil {
				slog.Warn("MC SaveLink failed", "discord_id", c.DiscordID, "error", err)
				continue
			}

			if s != nil && guildID != "" {
				if err := s.GuildMemberNickname(guildID, c.DiscordID, c.Username); err != nil {
					slog.Warn("MC could not rename member", "discord_id", c.DiscordID, "username", c.Username, "error", err)
				}
			}

			if s != nil {
				if ch, err := s.UserChannelCreate(c.DiscordID); err == nil {
					_, _ = s.ChannelMessageSend(ch.ID, lang.T("mc_link_poller_dm", "username", c.Username))
				}
			}

			slog.Info("MC account linked", "discord_id", c.DiscordID, "username", c.Username, "uuid", c.UUID)
		}
	}

	poll()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		for range ticker.C {
			poll()
		}
	}()
}

func minecraftCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{
			Name:        "mc",
			Description: "Minecraft server management & player profile",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name: "status", Description: "Check if the Minecraft server is reachable",
					Type: discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name: "command", Description: "Execute an RCON command on the Minecraft server",
					Type: discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionString, Name: "cmd", Description: "The command to run (e.g. list, whitelist add Steve)", Required: true},
					},
				},
				{
					Name: "players", Description: "List online players",
					Type: discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name: "say", Description: "Broadcast a message in-game",
					Type: discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionString, Name: "message", Description: "Message to broadcast", Required: true},
					},
				},
				{
					Name: "whitelist", Description: "Add or remove a player from the whitelist",
					Type: discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Type: discordgo.ApplicationCommandOptionString, Name: "action", Description: "add / remove", Required: true,
							Choices: []*discordgo.ApplicationCommandOptionChoice{
								{Name: "add", Value: "add"},
								{Name: "remove", Value: "remove"},
							},
						},
						{Type: discordgo.ApplicationCommandOptionString, Name: "player", Description: "Player name", Required: true},
					},
				},
				{
					Name: "link", Description: "Link your Discord account to your Minecraft account",
					Type: discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name: "unlink", Description: "Unlink your Minecraft account from Discord",
					Type: discordgo.ApplicationCommandOptionSubCommand,
				},
				{
					Name: "profile", Description: "View your linked Minecraft profile (balance, homes, inventory)",
					Type: discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{Type: discordgo.ApplicationCommandOptionUser, Name: "user", Description: "Discord user to check (admin only for others)"},
					},
				},
				{
					Name: "linked", Description: "(Admin) List all linked Discord ↔ Minecraft accounts",
					Type: discordgo.ApplicationCommandOptionSubCommand,
				},
			},
		},
	}
}

func handleMinecraftCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	cfg := getCfg()
	if !cfg.Minecraft.Enabled {
		respond(s, i, lang.T("mc_disabled"), true)
		return
	}

	sub := i.ApplicationCommandData().Options[0]

	switch sub.Name {
	case "link":
		handleMCLink(s, i)
		return
	case "unlink":
		handleMCUnlink(s, i)
		return
	case "profile":
		handleMCProfile(s, i, sub.Options)
		return
	case "linked":
		handleMCLinked(s, i)
		return
	}

	if !isAdmin(s, i) {
		respond(s, i, lang.T("no_permission_subcommand"), true)
		return
	}
	if getRCON() == nil {
		respond(s, i, lang.T("mc_rcon_not_init"), true)
		return
	}

	switch sub.Name {
	case "status":
		handleMCStatus(s, i)
	case "command":
		handleMCCommand(s, i, sub.Options)
	case "players":
		handleMCPlayers(s, i)
	case "say":
		handleMCSay(s, i, sub.Options)
	case "whitelist":
		handleMCWhitelist(s, i, sub.Options)
	}
}

func handleMCStatus(s *discordgo.Session, i *discordgo.InteractionCreate) {
	rcon := getRCON()
	if !rcon.IsConnected() {
		if err := rcon.Connect(); err != nil {
			respond(s, i, lang.T("mc_rcon_unreachable", "error", err.Error()), true)
			return
		}
	}
	respond(s, i, lang.T("mc_online"), true)
}

func handleMCCommand(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	om := subOptMap(opts)
	cmd := om["cmd"].StringValue()

	resp, err := getRCON().Command(cmd)
	if err != nil {
		respond(s, i, lang.T("mc_rcon_error", "error", err.Error()), true)
		return
	}
	if resp == "" {
		resp = "(no output)"
	}
	if len(resp) > 1900 {
		resp = resp[:1900] + "..."
	}
	respond(s, i, fmt.Sprintf("```\n> %s\n%s\n```", cmd, resp), true)
}

func handleMCPlayers(s *discordgo.Session, i *discordgo.InteractionCreate) {
	resp, err := getRCON().Command("list")
	if err != nil {
		respond(s, i, lang.T("mc_rcon_error", "error", err.Error()), true)
		return
	}
	respondEmbed(s, i, &discordgo.MessageEmbed{
		Title:       lang.T("mc_players_embed_title"),
		Description: resp,
		Color:       0x55FF55,
	}, true)
}

func handleMCSay(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	om := subOptMap(opts)
	message := om["message"].StringValue()

	_, err := getRCON().Command(fmt.Sprintf("say [Discord] %s: %s", i.Member.User.Username, message))
	if err != nil {
		respond(s, i, lang.T("mc_rcon_error", "error", err.Error()), true)
		return
	}
	respond(s, i, lang.T("mc_say_sent", "message", message), false)
}

func handleMCWhitelist(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	om := subOptMap(opts)
	action := om["action"].StringValue()
	player := om["player"].StringValue()

	player = strings.ReplaceAll(player, " ", "")
	if len(player) > 16 {
		respond(s, i, lang.T("mc_invalid_player"), true)
		return
	}

	resp, err := getRCON().Command(fmt.Sprintf("whitelist %s %s", action, player))
	if err != nil {
		respond(s, i, lang.T("mc_rcon_error", "error", err.Error()), true)
		return
	}
	respond(s, i, lang.T("mc_whitelist_result", "action", action, "player", player, "result", resp), true)
}

func handleMCLink(s *discordgo.Session, i *discordgo.InteractionCreate) {
	discordID := i.Member.User.ID
	store := getMCStore()

	if link, err := store.LoadLink(discordID); err == nil {
		respond(s, i, lang.T("mc_already_linked", "username", link.Username), true)
		return
	}

	code := generateLinkCode()
	expiresAt := time.Now().Add(10 * time.Minute)

	pendingLinksMu.Lock()
	for k, p := range pendingLinks {
		if p.discordID == discordID || time.Now().After(p.expiresAt) {
			delete(pendingLinks, k)
		}
	}
	pendingLinks[code] = pendingLink{discordID: discordID, expiresAt: expiresAt}
	pendingLinksMu.Unlock()

	if err := store.SavePendingCode(code, discordID, i.GuildID, expiresAt); err != nil {
		slog.Warn("MC SavePendingCode error", "error", err)
		respond(s, i, lang.T("mc_link_code_failed"), true)
		return
	}

	respondEmbed(s, i, &discordgo.MessageEmbed{
		Title:       lang.T("mc_link_embed_title"),
		Description: lang.T("mc_link_embed_description", "code", code),
		Color:       0x5865F2,
		Footer:      &discordgo.MessageEmbedFooter{Text: lang.T("mc_link_embed_footer")},
	}, true)
}

func handleMCUnlink(s *discordgo.Session, i *discordgo.InteractionCreate) {
	discordID := i.Member.User.ID
	store := getMCStore()

	link, err := store.LoadLink(discordID)
	if err != nil {
		respond(s, i, lang.T("mc_not_linked"), true)
		return
	}

	if err := store.DeleteLink(discordID); err != nil {
		respond(s, i, lang.T("mc_unlink_failed", "error", err.Error()), true)
		return
	}

	if i.GuildID != "" {
		if err := s.GuildMemberNickname(i.GuildID, discordID, ""); err != nil {
			slog.Warn("MC could not clear nickname", "discord_id", discordID, "error", err)
		}
	}

	respond(s, i, lang.T("mc_unlinked", "username", link.Username), true)
}

func handleMCProfile(s *discordgo.Session, i *discordgo.InteractionCreate, opts []*discordgo.ApplicationCommandInteractionDataOption) {
	targetDiscordID := i.Member.User.ID
	targetName := i.Member.User.Username
	store := getMCStore()

	if len(opts) > 0 {
		om := subOptMap(opts)
		if u, ok := om["user"]; ok {
			user := u.UserValue(s)
			if user.ID != i.Member.User.ID && !isAdmin(s, i) {
				respond(s, i, lang.T("mc_profile_admin_only"), true)
				return
			}
			targetDiscordID = user.ID
			targetName = user.Username
		}
	}

	link, err := store.LoadLink(targetDiscordID)
	if err != nil {
		if targetDiscordID == i.Member.User.ID {
			respond(s, i, lang.T("mc_profile_self_not_linked"), true)
		} else {
			respond(s, i, lang.T("mc_profile_other_not_linked", "user", targetName), true)
		}
		return
	}

	if getRCON() == nil {
		respond(s, i, lang.T("mc_profile_rcon_unavailable"), true)
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})

	balance := rconQuery(fmt.Sprintf("oe-discord balance %s", link.UUID))
	homes := rconQuery(fmt.Sprintf("oe-discord homes %s", link.UUID))
	onlineStatus := rconQuery(fmt.Sprintf("oe-discord online %s", link.UUID))

	embed := &discordgo.MessageEmbed{
		Title: lang.T("mc_profile_embed_title", "username", link.Username),
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: fmt.Sprintf("https://mc-heads.net/avatar/%s/64", link.UUID),
		},
		Fields: []*discordgo.MessageEmbedField{
			{Name: lang.T("mc_profile_field_username"), Value: link.Username, Inline: true},
			{Name: lang.T("mc_profile_field_status"), Value: onlineStatus, Inline: true},
			{Name: lang.T("mc_profile_field_linked_since"), Value: link.LinkedAt, Inline: true},
			{Name: lang.T("mc_profile_field_balance"), Value: balance, Inline: true},
			{Name: lang.T("mc_profile_field_homes"), Value: homes, Inline: false},
		},
		Color:  0x55FF55,
		Footer: &discordgo.MessageEmbedFooter{Text: "UUID: " + link.UUID},
	}

	_, _ = s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
		Flags:  discordgo.MessageFlagsEphemeral,
	})
}

func handleMCLinked(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if !isAdmin(s, i) {
		respond(s, i, lang.T("admin_only"), true)
		return
	}

	store := getMCStore()
	links, err := store.ListLinks()
	if err != nil || len(links) == 0 {
		respond(s, i, lang.T("mc_linked_none"), true)
		return
	}

	var sb strings.Builder
	sb.WriteString(lang.T("mc_linked_header", "count", fmt.Sprintf("%d", len(links))))
	for _, link := range links {
		sb.WriteString(lang.T("mc_linked_entry",
			"discord_id", link.DiscordID,
			"username", link.Username,
			"linked_at", link.LinkedAt,
		))
	}
	respond(s, i, sb.String(), true)
}

func rconQuery(cmd string) string {
	rcon := getRCON()
	if rcon == nil {
		return "*(RCON unavailable)*"
	}
	resp, err := rcon.Command(cmd)
	if err != nil {
		slog.Warn("RCON query failed", "command", cmd, "error", err)
		return "*(fetch failed)*"
	}
	if resp == "" {
		return "*(no data)*"
	}
	return resp
}
