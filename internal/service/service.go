// Package service owns the reservation rules exposed to HTTP clients.
package service

import (
	"context"
	"errors"
	"github.com/ZholdybekYertay/seatbook/internal/store"
	"regexp"
)

var ErrInvalid = errors.New("invalid input")
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,80}$`)

type Service struct{ Store *store.Store }

func ValidKey(key string) bool { return keyPattern.MatchString(key) }
func (s *Service) Reserve(ctx context.Context, user, seat int64, key string) (store.Booking, bool, error) {
	if user < 1 || seat < 1 || !ValidKey(key) {
		return store.Booking{}, false, ErrInvalid
	}
	return s.Store.Reserve(ctx, user, seat, key)
}
func (s *Service) Transition(ctx context.Context, user, id int64, action string) (store.Booking, error) {
	if user < 1 || id < 1 || (action != "confirm" && action != "cancel") {
		return store.Booking{}, ErrInvalid
	}
	return s.Store.Transition(ctx, user, id, action)
}
