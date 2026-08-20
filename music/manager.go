package music

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"discord-bot/config"

	"github.com/bwmarrin/discordgo"
)

type Song struct {
	Title     string
	URL       string
	StreamURL string
	Duration  int
	AddedBy   string
}

type Backend interface {
	Name() string

	ResolveSong(query string) (*Song, error)

	Play(vc *discordgo.VoiceConnection, song *Song, volume int, done chan<- struct{})
	Stop()
	SetVolume(vol int)
	Cleanup()
}

type GuildPlayer struct {
	mu      sync.Mutex
	guildID string

	queue      []*Song
	nowPlaying *Song
	playing    bool
	paused     bool
	volume     int

	voiceConn      *discordgo.VoiceConnection
	voiceChannelID string
	backend        Backend
	session        *discordgo.Session
	stopCh         chan struct{}
}

type Manager struct {
	mu      sync.RWMutex
	players map[string]*GuildPlayer
	backend Backend
	session *discordgo.Session
	cfg     *config.MusicConfig
}

func NewManager(s *discordgo.Session, cfg *config.MusicConfig) (*Manager, error) {
	var b Backend
	var err error

	switch cfg.Backend {
	case "direct":
		b, err = NewDirectBackend(&cfg.Direct)
		if err != nil {
			return nil, fmt.Errorf("direct backend: %w", err)
		}
		slog.Info("Using direct music backend", "ytdlp", cfg.Direct.YTDLPPath, "ffmpeg", cfg.Direct.FFmpegPath)

	case "lavalink":
		b, err = NewLavalinkBackend(&cfg.Lavalink, s)
		if err != nil {
			return nil, fmt.Errorf("lavalink backend: %w", err)
		}
		slog.Info("Using Lavalink music backend", "host", cfg.Lavalink.Host, "port", cfg.Lavalink.Port)

	default:
		return nil, fmt.Errorf("unknown music backend: %q (use \"direct\" or \"lavalink\")", cfg.Backend)
	}

	return &Manager{
		players: make(map[string]*GuildPlayer),
		backend: b,
		session: s,
		cfg:     cfg,
	}, nil
}

func (m *Manager) GetPlayer(guildID string) *GuildPlayer {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.players[guildID]
	if !ok {
		p = &GuildPlayer{
			guildID: guildID,
			volume:  m.cfg.DefaultVolume,
			backend: m.backend,
			session: m.session,
		}
		m.players[guildID] = p
	}
	return p
}

func (m *Manager) BackendName() string {
	return m.backend.Name()
}

func (m *Manager) ResolveSong(query string) (*Song, error) {
	return m.backend.ResolveSong(query)
}

func (m *Manager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.players {
		p.Stop()
	}
	m.backend.Cleanup()
}

func (p *GuildPlayer) Mu() *sync.Mutex {
	return &p.mu
}

func (p *GuildPlayer) NowPlaying() *Song {
	return p.nowPlaying
}

func (p *GuildPlayer) Queue() []*Song {
	return p.queue
}

func (p *GuildPlayer) Volume() int {
	return p.volume
}

func (p *GuildPlayer) IsPlaying() bool {
	return p.playing
}

func (p *GuildPlayer) IsPaused() bool {
	return p.paused
}

func (p *GuildPlayer) SetPausedState(v bool) {
	p.paused = v
}

func (p *GuildPlayer) JoinChannel(guildID, channelID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, isLavalink := p.backend.(*LavalinkBackend)

	if p.voiceConn != nil {
		if p.voiceChannelID == channelID {
			return nil
		}
		_ = p.voiceConn.Disconnect(context.Background())
	}

	ClearVoiceInfo(guildID)

	vc, err := p.session.ChannelVoiceJoin(context.Background(), guildID, channelID, false, false)
	if err != nil {
		return err
	}
	p.voiceConn = vc
	p.voiceChannelID = channelID

	if isLavalink {
		lb := p.backend.(*LavalinkBackend)
		lb.SetChannelID(channelID)
		time.Sleep(500 * time.Millisecond)
		vc.Kill()
		slog.Info("Closed discordgo voice WS for Lavalink", "guild", guildID)
	}

	return nil
}

func (p *GuildPlayer) Enqueue(song *Song, maxQueueSize int) (position int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.queue) >= maxQueueSize {
		return 0, fmt.Errorf("queue is full (%d/%d)", len(p.queue), maxQueueSize)
	}
	p.queue = append(p.queue, song)
	return len(p.queue), nil
}

func (p *GuildPlayer) PlayNext() {
	p.mu.Lock()

	if p.voiceConn == nil {
		p.playing = false
		p.mu.Unlock()
		return
	}

	if len(p.queue) == 0 {
		p.nowPlaying = nil
		p.playing = false
		p.mu.Unlock()

		go func() {
			time.Sleep(2 * time.Minute)
			p.mu.Lock()
			if !p.playing && p.voiceConn != nil {
				_ = p.voiceConn.Disconnect(context.Background())
				p.voiceConn = nil
				p.voiceChannelID = ""
			}
			p.mu.Unlock()
		}()
		return
	}

	song := p.queue[0]
	p.queue = p.queue[1:]
	p.nowPlaying = song
	p.playing = true
	p.paused = false

	vc := p.voiceConn
	vol := p.volume
	p.stopCh = make(chan struct{})
	p.mu.Unlock()

	done := make(chan struct{})
	go p.backend.Play(vc, song, vol, done)

	go func() {
		<-done
		p.mu.Lock()

		if p.nowPlaying == song {
			p.mu.Unlock()
			p.PlayNext()
		} else {
			p.mu.Unlock()
		}
	}()
}

func (p *GuildPlayer) Skip() *Song {
	p.mu.Lock()
	skipped := p.nowPlaying
	p.mu.Unlock()

	p.backend.Stop()

	return skipped
}

func (p *GuildPlayer) Stop() {
	p.mu.Lock()
	p.queue = nil
	p.nowPlaying = nil
	p.playing = false
	p.paused = false
	vc := p.voiceConn
	p.mu.Unlock()

	p.backend.Stop()

	if vc != nil {
		_ = vc.Disconnect(context.Background())
		p.mu.Lock()
		p.voiceConn = nil
		p.voiceChannelID = ""
		p.mu.Unlock()
		slog.Info("Disconnected from voice", "guild", vc.GuildID)
	}
}

func (p *GuildPlayer) SetVolume(vol int) {
	p.mu.Lock()
	p.volume = vol
	p.mu.Unlock()
	p.backend.SetVolume(vol)
}

func (p *GuildPlayer) Backend() Backend {
	return p.backend
}

func GetVoiceChannelOfUser(s *discordgo.Session, guildID, userID string) string {
	guild, err := s.State.Guild(guildID)
	if err != nil {
		return ""
	}
	for _, vs := range guild.VoiceStates {
		if vs.UserID == userID {
			return vs.ChannelID
		}
	}
	return ""
}
