package store

import (
	"net/url"
	"strings"
)

// UserFromInput достаёт имя пользователя из ввода: имя как есть или ссылка
// на страницу пользователя, где имя — последний непустой сегмент пути:
// https://site/<user>, https://site/<user>/, https://site/<раздел>/<user>/.
// Схема необязательна (site/<user>), query и fragment отбрасываются.
// Имя приводится к нижнему регистру: Alice и alice — один пользователь.
// Возвращает ErrInvalidUser, если имени нет или оно недопустимо.
func UserFromInput(input string) (string, error) {
	s := strings.TrimSpace(input)
	if strings.Contains(s, "/") {
		if !strings.Contains(s, "://") {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err != nil {
			return "", ErrInvalidUser
		}
		segments := strings.Split(strings.Trim(u.Path, "/"), "/")
		s = segments[len(segments)-1]
	}
	s = strings.ToLower(s)
	if !ValidUser(s) {
		return "", ErrInvalidUser
	}
	return s, nil
}
