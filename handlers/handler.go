package handlers

import (
	"discord-bot/config"
	"discord-bot/minecraft"
	"discord-bot/music"
	"discord-bot/storage"
)

type Handler struct {
	cfg      *config.Config
	db       storage.Database
	musicMgr *music.Manager
	rcon     *minecraft.Client
	mcStore  MCLinkStore
}

var globalHandler *Handler

func Init(cfg *config.Config, db storage.Database) *Handler {
	h := &Handler{cfg: cfg, db: db}
	globalHandler = h
	return h
}

func (h *Handler) SetMusic(m *music.Manager) {
	h.musicMgr = m
}

func (h *Handler) SetRCON(r *minecraft.Client) {
	h.rcon = r
}

func (h *Handler) SetMCStore(s MCLinkStore) {
	h.mcStore = s
}

func getCfg() *config.Config {
	if globalHandler != nil {
		return globalHandler.cfg
	}
	return storage.Cfg
}

func getMusicMgr() *music.Manager {
	if globalHandler != nil {
		return globalHandler.musicMgr
	}
	return nil
}

func getRCON() *minecraft.Client {
	if globalHandler != nil {
		return globalHandler.rcon
	}
	return nil
}

func getMCStore() MCLinkStore {
	if globalHandler != nil && globalHandler.mcStore != nil {
		return globalHandler.mcStore
	}
	return MCStore
}
