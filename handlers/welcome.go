package handlers

import (
	"log/slog"
	"strconv"
	"strings"

	"discord-bot/config"

	"github.com/bwmarrin/discordgo"
)

func RegisterWelcomeLeave(s *discordgo.Session) {
	cfg := getCfg()

	if cfg.Welcome.Enabled {
		s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
			handleWelcome(s, m)
		})
	}

	if cfg.Leave.Enabled {
		s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
			handleLeave(s, m)
		})
	}

	s.AddHandler(func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
		AssignJoinRole(s, m.GuildID, m.User.ID)
	})
}

func handleWelcome(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	cfg := getCfg()
	if cfg.Welcome.ChannelID == "" || cfg.Welcome.ChannelID == "PUT_WELCOME_CHANNEL_ID_HERE" {
		return
	}

	embed := buildWelcomeLeaveEmbed(s, &cfg.Welcome, m.User, m.GuildID)
	if _, err := s.ChannelMessageSendEmbed(cfg.Welcome.ChannelID, embed); err != nil {
		slog.Warn("Failed to send welcome message", "error", err)
	}
}

func handleLeave(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
	cfg := getCfg()
	if cfg.Leave.ChannelID == "" || cfg.Leave.ChannelID == "PUT_LEAVE_CHANNEL_ID_HERE" {
		return
	}

	embed := buildWelcomeLeaveEmbed(s, &cfg.Leave, m.User, m.GuildID)
	if _, err := s.ChannelMessageSendEmbed(cfg.Leave.ChannelID, embed); err != nil {
		slog.Warn("Failed to send leave message", "error", err)
	}
}

func buildWelcomeLeaveEmbed(s *discordgo.Session, cfg *config.WelcomeLeaveConfig, user *discordgo.User, guildID string) *discordgo.MessageEmbed {
	msg := cfg.Embed.Message
	msg = strings.ReplaceAll(msg, "%joined_user%", user.Mention())
	msg = strings.ReplaceAll(msg, "%username%", user.Username)

	title := cfg.Embed.Title
	title = strings.ReplaceAll(title, "%joined_user%", user.Username)
	title = strings.ReplaceAll(title, "%username%", user.Username)

	colour := 0
	hex := strings.TrimPrefix(cfg.Embed.Colour, "#")
	if v, err := strconv.ParseInt(hex, 16, 64); err == nil {
		colour = int(v)
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: msg,
		Color:       colour,
	}

	if cfg.Embed.Thumbnail != "" {
		thumbURL := cfg.Embed.Thumbnail
		if strings.EqualFold(thumbURL, "BOT") {
			if s.State != nil && s.State.User != nil {
				thumbURL = s.State.User.AvatarURL("256")
			} else {
				thumbURL = ""
			}
		} else if strings.EqualFold(thumbURL, "USER") {
			thumbURL = user.AvatarURL("256")
		}
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: thumbURL}
	}

	if cfg.Embed.ImageEnabled && cfg.Embed.ImageURL != "" {
		embed.Image = &discordgo.MessageEmbedImage{URL: cfg.Embed.ImageURL}
	}

	_ = guildID
	return embed
}
