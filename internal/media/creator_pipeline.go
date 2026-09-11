package media

import (
	"errors"
	"sync"
)

var ErrTransition = errors.New("media asset transition rejected")

type Stage string

const (
	StageIngested   Stage = "ingested"
	StageProcessing Stage = "processing"
	StageReady      Stage = "ready"
	StageDelivered  Stage = "delivered"
)

type Asset struct {
	ID      string `json:"id"`
	Creator string `json:"creator"`
	Title   string `json:"title"`
	Stage   Stage  `json:"stage"`
}

type Pipeline struct {
	mu     sync.Mutex
	assets map[string]Asset
}

func NewPipeline() *Pipeline { return &Pipeline{assets: make(map[string]Asset)} }

func (p *Pipeline) Ingest(asset Asset) (Asset, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if asset.ID == "" || asset.Creator == "" || asset.Title == "" {
		return Asset{}, ErrTransition
	}
	if _, exists := p.assets[asset.ID]; exists {
		return Asset{}, ErrTransition
	}
	asset.Stage = StageIngested
	p.assets[asset.ID] = asset
	return asset, nil
}

func (p *Pipeline) Advance(id string, target Stage) (Asset, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	asset, exists := p.assets[id]
	if !exists || nextStage(asset.Stage) != target {
		return Asset{}, ErrTransition
	}
	asset.Stage = target
	p.assets[id] = asset
	return asset, nil
}

func nextStage(current Stage) Stage {
	switch current {
	case StageIngested:
		return StageProcessing
	case StageProcessing:
		return StageReady
	case StageReady:
		return StageDelivered
	default:
		return ""
	}
}
