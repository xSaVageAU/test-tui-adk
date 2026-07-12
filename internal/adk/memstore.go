package adk

import (
	"context"
	"strings"
	"time"

	"google.golang.org/genai"
	"gorm.io/gorm"

	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/session"
)

// memoryRow is one memory entry's on-disk row — one per event that had
// text content, mirroring what memory.InMemoryService keeps in its
// process-lifetime map, just persisted instead of wiped on restart.
type memoryRow struct {
	ID        string `gorm:"primaryKey"`
	AppName   string `gorm:"index:idx_memory_scope"`
	UserID    string `gorm:"index:idx_memory_scope"`
	SessionID string `gorm:"index:idx_memory_scope"`
	Author    string
	Text      string
	Timestamp time.Time
}

func (memoryRow) TableName() string { return "memory_entries" }

// sqliteMemoryService is a small, from-scratch memory.Service. ADK only
// ships memory.InMemoryService (wiped on restart) and a Vertex AI Memory
// Bank-backed one (needs GCP credentials) — this fills the "just persist
// it locally" gap, deliberately mirroring InMemoryService's own matching
// approach (plain case-insensitive word-overlap, no stemming/ranking —
// see memory/inmemory.go's extractWords/checkMapsIntersect, read to keep
// this a faithful drop-in rather than a different design) so behavior
// wouldn't visibly change if ADK ever ships a database-backed option to
// switch to instead.
type sqliteMemoryService struct {
	db *gorm.DB
}

func newMemoryService(db *gorm.DB) (memory.Service, error) {
	if err := db.AutoMigrate(&memoryRow{}); err != nil {
		return nil, err
	}
	return &sqliteMemoryService{db: db}, nil
}

// AddSessionToMemory replaces — not appends — this session's rows with a
// fresh extraction of its current full event list, same as upstream's
// AddSessionToMemory. That replace semantics matters here specifically
// because this app calls it after every turn (see runStream): re-deriving
// from the session's current state each time means calling it repeatedly
// just keeps the extraction current, rather than growing duplicate rows
// turn over turn.
func (s *sqliteMemoryService) AddSessionToMemory(ctx context.Context, sess session.Session) error {
	appName := sess.AppName()

	var rows []memoryRow
	for event := range sess.Events().All() {
		if event.Content == nil {
			continue
		}
		var text strings.Builder
		for _, part := range event.Content.Parts {
			text.WriteString(part.Text)
		}
		if text.Len() == 0 {
			continue
		}
		rows = append(rows, memoryRow{
			ID:        event.ID,
			AppName:   appName,
			UserID:    sess.UserID(),
			SessionID: sess.ID(),
			Author:    event.Author,
			Text:      text.String(),
			Timestamp: event.Timestamp,
		})
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("app_name = ? AND user_id = ? AND session_id = ?", appName, sess.UserID(), sess.ID()).
			Delete(&memoryRow{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
}

// SearchMemory matches memory.InMemoryService's algorithm exactly: any
// shared word (case-insensitive, whitespace-split) between the query and
// a stored entry's text counts as a match — deliberately unsophisticated,
// same as upstream, not a design shortcut specific to this copy.
func (s *sqliteMemoryService) SearchMemory(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	var rows []memoryRow
	if err := s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ?", req.AppName, req.UserID).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	queryWords := extractWords(req.Query)
	resp := &memory.SearchResponse{}
	for _, row := range rows {
		if !wordsIntersect(extractWords(row.Text), queryWords) {
			continue
		}
		resp.Memories = append(resp.Memories, memory.Entry{
			ID:        row.ID,
			Content:   &genai.Content{Parts: []*genai.Part{{Text: row.Text}}},
			Author:    row.Author,
			Timestamp: row.Timestamp,
		})
	}
	return resp, nil
}

func extractWords(text string) map[string]struct{} {
	words := make(map[string]struct{})
	for w := range strings.FieldsSeq(text) {
		words[strings.ToLower(w)] = struct{}{}
	}
	return words
}

func wordsIntersect(a, b map[string]struct{}) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	for w := range a {
		if _, ok := b[w]; ok {
			return true
		}
	}
	return false
}
