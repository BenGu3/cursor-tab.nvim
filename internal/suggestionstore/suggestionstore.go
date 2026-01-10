package suggestionstore

import (
	"sync"
)

type RangeInfo struct {
	StartLine   int32 `json:"start_line"`
	StartColumn int32 `json:"start_column"`
	EndLine     int32 `json:"end_line"`
	EndColumn   int32 `json:"end_column"`
}

type Suggestion struct {
	Text                   string     `json:"text"`
	Range                  *RangeInfo `json:"range,omitempty"`
	BindingID              string     `json:"binding_id,omitempty"`
	ShouldRemoveLeadingEol bool       `json:"should_remove_leading_eol,omitempty"`
	NextSuggestionID       string     `json:"next_suggestion_id,omitempty"`
}

type Store struct {
	mu          sync.RWMutex
	suggestions map[string]*Suggestion
}

func NewStore() *Store {
	return &Store{
		suggestions: make(map[string]*Suggestion),
	}
}

func (s *Store) Store(suggestionID string, suggestion *Suggestion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.suggestions[suggestionID] = suggestion
}

func (s *Store) Get(suggestionID string) *Suggestion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.suggestions[suggestionID]
}

func (s *Store) Delete(suggestionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.suggestions, suggestionID)
}

// Keys returns all suggestion IDs currently in the store (for debugging)
func (s *Store) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.suggestions))
	for k := range s.suggestions {
		keys = append(keys, k)
	}
	return keys
}

// GetAll returns all suggestions currently in the store (for debugging)
func (s *Store) GetAll() map[string]*Suggestion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Make a copy to avoid race conditions
	all := make(map[string]*Suggestion, len(s.suggestions))
	for k, v := range s.suggestions {
		all[k] = v
	}
	return all
}
