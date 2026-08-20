package main

import (
	"context"
	"discord-bot/bot"
	"discord-bot/config"
	"discord-bot/handlers"
	"discord-bot/lang"
	"discord-bot/minecraft"
	"discord-bot/music"
	"discord-bot/payments"
	"discord-bot/storage"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	defer func() {
		if r := recover(); r != nil {
			slog.Error("PANIC", "panic", r, "stack", string(debug.Stack()))
			fmt.Fprintf(os.Stderr, "PANIC: %v\n%s\n", r, debug.Stack())
			os.Exit(1)
		}
	}()

	run()
}

const exitCodeScheduledRestart = 2

func run() {
	configPath := flag.String("config", "config.json", "Path to config file")
	cleanup := flag.Bool("cleanup", false, "Remove slash commands on shutdown")
	restartFlag := flag.Int("restart", -1, "Auto-restart interval in minutes (0 = off, -1 = use config value)")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}
	if cfg.Discord.Token == "" || cfg.Discord.Token == "YOUR_DISCORD_BOT_TOKEN_HERE" {
		slog.Error("Set your bot token in config.json → discord.token")
		os.Exit(1)
	}

	lang.Load("lang.yml")

	storage.Cfg = cfg
	storage.LoadAllGuildStates()

	if err := storage.InitDB(&cfg.Database); err != nil {
		slog.Warn("Database init failed, falling back to JSON-only storage", "error", err)
	} else {
		defer storage.DB.Close()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hh := handlers.Init(cfg, storage.DB)

	if cfg.Minecraft.Enabled {
		handlers.InitMCLinkStore()

		rcon := minecraft.NewClient(cfg.Minecraft.RCONAddress, cfg.Minecraft.RCONPort, cfg.Minecraft.RCONPassword)
		if err := rcon.Connect(); err != nil {
			slog.Warn("Minecraft RCON connection failed, commands will retry on use", "error", err)
		} else {
			slog.Info("Minecraft RCON connected")
		}
		hh.SetRCON(rcon)
	}

	b, err := bot.New(cfg)
	if err != nil {
		slog.Error("Failed to create bot", "error", err)
		os.Exit(1)
	}

	handlers.Register(b.Session)
	handlers.RegisterWelcomeLeave(b.Session)
	handlers.RegisterNoPing(b.Session, cfg)
	handlers.RegisterCounting(b.Session, cfg)
	handlers.RegisterCustomCommands(cfg)
	handlers.RegisterInviteTracker(b.Session)

	if cfg.ChatBridge.Enabled {
		bridge, err := handlers.NewChatBridge(b.Session, &cfg.ChatBridge)
		if err != nil {
			slog.Warn("ChatBridge init failed", "error", err)
		} else {
			bridge.Start()
			defer bridge.Stop()
		}
	}

	b.Session.AddHandler(func(s *discordgo.Session, e *discordgo.VoiceStateUpdate) {
		if s.State != nil && s.State.User != nil && e.UserID == s.State.User.ID {
			music.UpdateVoiceState(e.GuildID, e.SessionID)
		}
	})

	b.Session.AddHandler(func(s *discordgo.Session, e *discordgo.VoiceServerUpdate) {
		endpoint := ""
		if e.Endpoint != nil {
			endpoint = *e.Endpoint
		}
		music.UpdateVoiceServer(e.GuildID, e.Token, endpoint)
	})

	if err := b.Start(); err != nil {
		slog.Error("Failed to start bot", "error", err)
		os.Exit(1)
	}
	defer b.Stop()

	var guildID string
	if cfg.Discord.GuildID != "" {
		guildID = cfg.Discord.GuildID
	} else if b.Session.State != nil && len(b.Session.State.Guilds) > 0 {
		guildID = b.Session.State.Guilds[0].ID
	}

	if guildID != "" {
		gs := storage.GetGuild(guildID)
		handlers.RestoreGiveawayTimers(b.Session, gs)
		slog.Info("Giveaway timers restored")
	}

	if cfg.Minecraft.Enabled {
		handlers.StartLinkPoller(b.Session, guildID)
	}

	if cfg.Music.Enabled {
		mgr, err := music.NewManager(b.Session, &cfg.Music)
		if err != nil {
			slog.Warn("Music system init failed", "error", err)
			slog.Info("Music commands will show an error — fix the issue and restart")
		} else {
			hh.SetMusic(mgr)
			defer mgr.Cleanup()
		}
	}

	payments.Start(cfg, b.Session, ctx)

	registered := b.RegisterCommands(handlers.Commands(cfg))

	restartMinutes := cfg.AutoRestartMinutes
	if *restartFlag >= 0 {
		restartMinutes = *restartFlag
	}
	if restartMinutes > 0 {
		slog.Info("Auto-restart enabled", "interval_minutes", restartMinutes)
		go func() {
			time.Sleep(time.Duration(restartMinutes) * time.Minute)
			slog.Info("Scheduled restart", "interval_minutes", restartMinutes)
			if *cleanup {
				b.CleanupCommands(registered)
			}
			b.Stop()
			os.Exit(exitCodeScheduledRestart)
		}()
	}

	slog.Info("Bot is running — press Ctrl+C to exit")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("Shutting down")
	if *cleanup {
		b.CleanupCommands(registered)
	}
}
