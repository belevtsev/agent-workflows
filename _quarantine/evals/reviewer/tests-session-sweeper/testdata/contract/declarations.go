// Declaration extract for package session (bodies stripped).
package session

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidIdle = errors.New("session: idle timeout must be positive")
var ErrNilClock = errors.New("session: clock must not be nil")
var ErrClosed = errors.New("session: store is closed")

type Store struct{}

func NewStore(idle time.Duration, clock func() time.Time) (*Store, error)
func (s *Store) Touch(id string) error
func (s *Store) Active(id string) bool
func (s *Store) Sweep(ctx context.Context) (int, error)
func (s *Store) Len() int
func (s *Store) Close() error
