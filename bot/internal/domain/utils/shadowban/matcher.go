package shadowban

import (
	"strings"

	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/entity"
	"github.com/Badsnus/cu-clubs-bot/bot/internal/domain/valueobject"
)

type Matcher struct {
	pairs map[string]struct{}
}

func NewMatcher(entries []string) *Matcher {
	pairs := make(map[string]struct{}, len(entries)*2)

	for _, entry := range entries {
		parts := strings.Fields(entry)
		if len(parts) < 2 {
			continue
		}

		first := normalizeToken(parts[0])
		second := normalizeToken(parts[1])
		if first == "" || second == "" {
			continue
		}

		pairs[pairKey(first, second)] = struct{}{}
		pairs[pairKey(second, first)] = struct{}{}
	}

	return &Matcher{pairs: pairs}
}

func (m *Matcher) MatchUser(user entity.User) bool {
	return m.MatchFIO(user.FIO)
}

func (m *Matcher) MatchFIO(fio valueobject.FIO) bool {
	if m == nil || len(m.pairs) == 0 {
		return false
	}

	_, ok := m.pairs[pairKey(normalizeToken(fio.Name), normalizeToken(fio.Surname))]
	return ok
}

func normalizeToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "ё", "е")
	return value
}

func pairKey(first, second string) string {
	return first + " " + second
}
