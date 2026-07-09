package storage

import (
	"encoding/json"
	"os"
	"path/filepath"

	"pvsk-pipeline/internal/models"
)

type Manager struct {
	Root string
}

func New(root string) Manager {
	return Manager{Root: root}
}

func (m Manager) PaperDir(paperID string) string {
	return filepath.Join(m.Root, paperID)
}

func (m Manager) EnsurePaperDir(paperID string) (string, error) {
	dir := m.PaperDir(paperID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (m Manager) WriteMetadata(p models.Paper) (string, error) {
	dir, err := m.EnsurePaperDir(p.ID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "metadata.json")
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o644)
}
